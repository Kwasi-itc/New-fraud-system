package mysqlruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"github.com/google/uuid"
)

func (p *LocalCreation) CreateField(ctx context.Context, input service.CreateFieldInput, op uuid.UUID) (datamodel.Field, error) {
	var result datamodel.Field
	if err := localAuthority(ctx, nil); err != nil {
		return result, err
	}
	if err := datamodel.ValidateFieldCreate(input.Name, input.DataType, input.IsEnum, input.IsUnique); err != nil {
		return result, err
	}
	if err := service.ValidateFieldSeeds(input); err != nil {
		return result, err
	}
	var tenant mysqlstore.UUID
	if err := p.Target.QueryRowContext(ctx, "SELECT tenant_id FROM fraud.data_model_tables WHERE id=? AND NOT archived", mysqlstore.UUID(input.TableID)).Scan(&tenant); err != nil {
		return result, err
	}
	tenantID := uuid.UUID(tenant)
	sum := sha256.Sum256([]byte("local-field/" + input.TableID.String()))
	err := mysqlstore.WithDDLLock(ctx, p.Target, "dm_local_field_"+hex.EncodeToString(sum[:20]), func(_ *sql.Conn) error {
		actor := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
		fieldID := uuid.NewSHA1(op, []byte("field/"+tenantID.String()+"/"+input.TableID.String()))
		physical := store.PhysicalTable{TableID: input.TableID}
		var expected mysqlstore.UUID
		var raw []byte
		var state string
		var storedHash []byte
		err := p.Target.QueryRowContext(ctx, "SELECT expected_version,desired_shape,state,request_hash FROM fraud.data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND operation_id=?", tenant, mysqlstore.UUID(input.TableID), mysqlstore.UUID(op)).Scan(&expected, &raw, &state, &storedHash)
		replay := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := p.Target.QueryRowContext(ctx, "SELECT physical_name,epoch FROM fraud.data_model_physical_tables WHERE tenant_id=? AND table_id=?", tenant, mysqlstore.UUID(input.TableID)).Scan(&physical.Name, &physical.Epoch); err != nil {
			return err
		}
		var plan store.PhysicalDDLPlan
		if replay {
			if err := json.Unmarshal(raw, &plan); err != nil {
				return err
			}
		} else {
			if err := p.Target.QueryRowContext(ctx, "SELECT published_version FROM fraud.data_model_physical_tables WHERE tenant_id=? AND table_id=? AND write_state='open'", tenant, mysqlstore.UUID(input.TableID)).Scan(&expected); err != nil {
				return err
			}
		}
		condition := store.PublicationCondition{Mode: store.PublicationExactVersion, Version: uuid.UUID(expected)}
		hash, err := service.PreparedFieldCreationRequestHash(service.PublicationRequestContext{TenantID: tenantID, TableID: input.TableID, ActorID: actor, Condition: condition}, fieldID, input)
		if err != nil {
			return err
		}
		if replay && !bytes.Equal(storedHash, hash[:]) {
			return store.ErrPublicationOperationConflict
		}
		if !replay {
			var previous store.PhysicalDDLPlan
			// The immediately preceding physical epoch identifies its plan exactly;
			// presentation updates do not increment this epoch.
			if err := p.Target.QueryRowContext(ctx, "SELECT desired_shape FROM fraud.data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND expected_epoch=? AND state='published'", tenant, mysqlstore.UUID(input.TableID), physical.Epoch-1).Scan(&raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &previous); err != nil {
				return err
			}
			at := localClock{}.Now()
			field := datamodel.Field{ID: fieldID, TenantID: tenantID, TableID: input.TableID, Name: datamodel.NormalizeName(input.Name), Alias: strings.TrimSpace(input.Alias), Description: input.Description, DataType: input.DataType, Nullable: input.Nullable, IsUnique: input.IsUnique, IsEnum: input.IsEnum, CreatedAt: at, UpdatedAt: at}
			mapped := []store.MappedPhysicalField{}
			err := mysqlstore.Snapshot(ctx, p.Target, func(tx *sql.Tx) error {
				repos, err := store.NewMetadataRepositories(tx, "fraud", tenantID, 1000, 4<<20)
				if err != nil {
					return err
				}
				fields, err := repos.Fields.ListByTable(ctx, input.TableID)
				if err != nil {
					return err
				}
				registry, err := store.NewPhysicalRegistry(tx, "fraud", tenantID, 1000)
				if err != nil {
					return err
				}
				columns, err := registry.Fields(ctx, input.TableID)
				if err != nil {
					return err
				}
				byID := map[uuid.UUID][]store.PhysicalField{}
				for _, column := range columns {
					byID[column.FieldID] = append(byID[column.FieldID], column)
				}
				for _, field := range fields {
					mapped = append(mapped, store.MappedPhysicalField{Field: field, Components: byID[field.ID]})
				}
				return nil
			})
			if err != nil {
				return err
			}
			addition, err := store.PreviewFieldMapping(field)
			if err != nil {
				return err
			}
			mapped = append(mapped, addition)
			planner, err := store.NewPhysicalPlanner("fraud", tenantID, physical, mapped)
			if err != nil {
				return err
			}
			var statements []store.PhysicalStatement
			if input.IsUnique && input.DataType == datamodel.DataTypeString {
				statements, err = planner.AddUniqueText(fieldID, previous.Creation.Definer)
			} else {
				var statement store.PhysicalStatement
				statement, err = planner.AddFields([]uuid.UUID{fieldID})
				statements = []store.PhysicalStatement{statement}
			}
			if err != nil {
				return err
			}
			reference, err := p.nativeReferences(ctx, physical, previous, statements)
			if err != nil {
				return err
			}
			plan, err = planner.BindRecordChanges(previous, statements, reference)
			if err != nil {
				return err
			}
			raw, err = json.Marshal(plan)
			if err != nil {
				return err
			}
			intents, err := store.NewDDLIntentStore(p.Target, "fraud", tenantID, 1<<20)
			if err != nil {
				return err
			}
			version := uuid.UUID(expected)
			err = mysqlstore.Mutation(ctx, p.Target, func(tx *sql.Tx) error {
				var locked mysqlstore.UUID
				if err := tx.QueryRowContext(ctx, "SELECT id FROM fraud.data_model_tables WHERE tenant_id=? AND id=? AND NOT archived FOR UPDATE", tenant, mysqlstore.UUID(input.TableID)).Scan(&locked); err != nil {
					return err
				}
				if _, err := intents.PrepareTx(ctx, tx, store.DDLIntent{TableID: input.TableID, OperationID: op, RequestHash: hash, ActorKind: "api", ActorID: actor.String(), ExpectedEpoch: physical.Epoch, ExpectedVersion: &version, Shape: raw}); err != nil {
					return err
				}
				var status string
				if err := tx.QueryRowContext(ctx, "SELECT status FROM fraud.data_model_tenants WHERE id=?", tenant).Scan(&status); err != nil {
					return err
				}
				if status != "active" {
					return store.ErrModelUnavailable
				}
				if !input.Nullable {
					name, err := mysqlstore.Identifier(physical.Name)
					if err != nil {
						return err
					}
					var exists int
					err = tx.QueryRowContext(ctx, "SELECT 1 FROM fraud."+name+" LIMIT 1").Scan(&exists)
					if err == nil {
						return store.ErrPhysicalPrecondition
					}
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
				}
				repos, err := store.NewMetadataRepositories(tx, "fraud", tenantID, 1000, 4<<20)
				if err != nil {
					return err
				}
				if err := repos.Fields.Create(ctx, field); err != nil {
					return err
				}
				for i, seed := range input.EnumValues {
					value := datamodel.FieldEnumValue{ID: uuid.NewSHA1(fieldID, []byte(fmt.Sprintf("enum/%d", i))), TenantID: tenantID, FieldID: fieldID, Value: seed.Value, Label: seed.Label, SortOrder: seed.SortOrder, CreatedAt: at, UpdatedAt: at}
					if err := repos.Enums.Create(ctx, value); err != nil {
						return err
					}
				}
				registry, err := store.NewPhysicalRegistry(tx, "fraud", tenantID, 1000)
				if err != nil {
					return err
				}
				return registry.RegisterFields(ctx, input.TableID, []uuid.UUID{fieldID})
			})
			if err != nil {
				return err
			}
		}
		installerHost, err := ec2test.InstallerHost()
		if err != nil {
			return err
		}
		installer := installation.RecordInstallerPolicy{Principal: installation.Definer{User: "lcn_" + strings.ReplaceAll(input.TableID.String(), "-", "")[:24], Host: installerHost}, Table: physical.Name, NativeChanges: true, TextGuards: plan.InstallsTextGuards()}
		if state != "published" {
			if err := p.executeNative(ctx, tenantID, input.TableID, op, installer); err != nil {
				return err
			}
		}
		metadata, err := store.NewTransactionManager(p.Target, "fraud", tenantID, 1000, 4<<20, localTenantAuthority, nil)
		if err != nil {
			return err
		}
		publication, err := store.NewPublicationStore(p.Target, "fraud", 1000, 1000, 4<<20)
		if err != nil {
			return err
		}
		request := ports.PhysicalPublicationRequest{TableID: input.TableID, OperationID: op, RequestHash: hash, Condition: condition, ActorKind: "api", ActorID: actor.String()}
		manager, err := store.NewPhysicalPublicationManager(metadata, publication, physical, localPhysicalPolicies(physical), func(ctx context.Context, id uuid.UUID, actual ports.PhysicalPublicationRequest) error {
			if id != tenantID || actual != request {
				return store.ErrPhysicalConflict
			}
			return localAuthority(ctx, nil)
		})
		if err != nil {
			return err
		}
		defs := []installation.Definer{}
		for _, name := range []string{"local_dm_guard", "local_dm_creation", "local_dm_result", "local_dm_record", "local_dm_contract"} {
			defs = append(defs, installation.Definer{User: name, Host: "localhost"})
		}
		readiness, err := installation.NewExactKeyReadinessWithLookups(p.Target, p.Plan, defs[0], defs[1], defs[2], defs[3], defs[4])
		if err != nil {
			return err
		}
		manager, err = manager.WithCreationReadiness(readiness, installer)
		if err != nil {
			return err
		}
		owner := service.NewFieldService(nil, nil, nil, nil, nil, nil, nil, nil, nil, localIDs{}, localClock{})
		result, err = owner.PublishPreparedCreationResult(ctx, tenantID, fieldID, input, request, manager)
		return err
	})
	return result, err
}

func localPhysicalPolicies(table store.PhysicalTable) []store.PhysicalAccessPolicy {
	suffix := strings.ReplaceAll(table.TableID.String(), "-", "")[:24]
	return []store.PhysicalAccessPolicy{
		{Mode: "writer", Account: installation.AccountPolicy{User: "lcw_" + suffix, Host: "localhost", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: table.Name, Privileges: []string{"SELECT", "INSERT", "UPDATE"}}}}},
		{Mode: "ddl", Account: installation.AccountPolicy{User: "lcd_" + suffix, Host: "localhost", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: table.Name, Privileges: []string{"SELECT", "CREATE", "ALTER", "INDEX"}}}}},
	}
}

func (p *LocalCreation) executeNative(ctx context.Context, tenant, table, op uuid.UUID, policy installation.RecordInstallerPolicy) (result error) {
	account, err := policy.Principal.SQLAccount()
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(p.Secret + policy.Principal.User))
	password := hex.EncodeToString(sum[:])
	if _, err = p.Target.ExecContext(ctx, "CREATE USER IF NOT EXISTS "+account+" IDENTIFIED BY '"+password+"' ACCOUNT LOCK"); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := p.Target.ExecContext(cleanup, "ALTER USER "+account+" ACCOUNT LOCK")
		result = errors.Join(result, err)
		if policy.BackfillColumn != "" {
			column, e := mysqlstore.Identifier(policy.BackfillColumn)
			if e != nil {
				result = errors.Join(result, e)
			} else {
				table, e := mysqlstore.Identifier(policy.Table)
				if e != nil {
					result = errors.Join(result, e)
				} else {
					_, e = p.Target.ExecContext(cleanup, "REVOKE IF EXISTS UPDATE ("+column+") ON fraud."+table+" FROM "+account)
					result = errors.Join(result, e)
				}
			}
		}
		// Publication independently checks that the installer is inactive.
		if policy.TextGuards {
			_, revokeErr := p.Target.ExecContext(cleanup, "REVOKE IF EXISTS SET_ANY_DEFINER ON *.* FROM "+account)
			result = errors.Join(result, revokeErr)
		}
	}()
	grants, err := policy.ActiveGrants("fraud")
	if err != nil {
		return err
	}
	for _, q := range grants {
		if _, err := p.Target.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	if _, err := p.Target.ExecContext(ctx, "ALTER USER "+account+" IDENTIFIED BY '"+password+"' ACCOUNT UNLOCK"); err != nil {
		return err
	}
	login, err := OpenLocalSocket(ctx, "/target-socket/mysqld.sock", policy.Principal.User, password)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, login.Close()) }()
	executor, err := store.NewPhysicalExecutor(login, "fraud", tenant)
	if err != nil {
		return err
	}
	return executor.ExecuteSteps(ctx, table, op)
}
