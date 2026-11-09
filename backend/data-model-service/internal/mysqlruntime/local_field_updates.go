package mysqlruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func (p *LocalCreation) updateFieldConstraints(ctx context.Context, input service.UpdateFieldInput, op uuid.UUID) (datamodel.Field, error) {
	return p.mutatePhysicalField(ctx, input, op, false)
}

func (p *LocalCreation) mutatePhysicalField(ctx context.Context, input service.UpdateFieldInput, op uuid.UUID, remove bool) (datamodel.Field, error) {
	var command any = input
	if remove {
		command = service.DeleteFieldInput{FieldID: input.FieldID}
	}
	var result datamodel.Field
	if err := localAuthority(ctx, nil); err != nil {
		return result, err
	}
	var tenant, table mysqlstore.UUID
	if err := p.Target.QueryRowContext(ctx, "SELECT tenant_id,table_id FROM fraud.data_model_fields WHERE id=? AND NOT archived", mysqlstore.UUID(input.FieldID)).Scan(&tenant, &table); err != nil {
		return result, err
	}
	tenantID, tableID := uuid.UUID(tenant), uuid.UUID(table)
	sum := sha256.Sum256([]byte("local-field/" + tableID.String()))
	err := mysqlstore.WithDDLLock(ctx, p.Target, "dm_local_field_"+hex.EncodeToString(sum[:20]), func(_ *sql.Conn) error {
		actor := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
		fieldID := input.FieldID
		physical := store.PhysicalTable{TableID: tableID}
		var expected mysqlstore.UUID
		var raw []byte
		var state string
		var storedHash []byte
		err := p.Target.QueryRowContext(ctx, "SELECT expected_version,desired_shape,state,request_hash FROM fraud.data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND operation_id=?", tenant, mysqlstore.UUID(tableID), mysqlstore.UUID(op)).Scan(&expected, &raw, &state, &storedHash)
		replay := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := p.Target.QueryRowContext(ctx, "SELECT physical_name,epoch FROM fraud.data_model_physical_tables WHERE tenant_id=? AND table_id=?", tenant, mysqlstore.UUID(tableID)).Scan(&physical.Name, &physical.Epoch); err != nil {
			return err
		}
		if !replay {
			var original mysqlstore.UUID
			err := p.Target.QueryRowContext(ctx, "SELECT expected_version FROM fraud.data_model_publication_operations WHERE tenant_id=? AND table_id=? AND operation_id=?", tenant, table, mysqlstore.UUID(op)).Scan(&original)
			if err == nil {
				if remove {
					return store.ErrPublicationOperationConflict
				}
				result, err = p.publishFieldCatalog(ctx, tenantID, tableID, op, store.PublicationCondition{Mode: store.PublicationExactVersion, Version: uuid.UUID(original)}, input)
				return err
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		var plan store.PhysicalDDLPlan
		if replay {
			if err := json.Unmarshal(raw, &plan); err != nil {
				return err
			}
		} else {
			if err := p.Target.QueryRowContext(ctx, "SELECT published_version FROM fraud.data_model_physical_tables WHERE tenant_id=? AND table_id=? AND write_state='open'", tenant, mysqlstore.UUID(tableID)).Scan(&expected); err != nil {
				return err
			}
		}
		condition := store.PublicationCondition{Mode: store.PublicationExactVersion, Version: uuid.UUID(expected)}
		hash, err := service.PublicationRequestHash(service.PublicationRequestContext{TenantID: tenantID, TableID: tableID, ActorID: actor, Condition: condition}, command)
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
			if err := p.Target.QueryRowContext(ctx, "SELECT desired_shape FROM fraud.data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND expected_epoch=? AND state='published'", tenant, mysqlstore.UUID(tableID), physical.Epoch-1).Scan(&raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &previous); err != nil {
				return err
			}
			mapped := []store.MappedPhysicalField{}
			err := mysqlstore.Snapshot(ctx, p.Target, func(tx *sql.Tx) error {
				repos, err := store.NewMetadataRepositories(tx, "fraud", tenantID, 1000, 4<<20)
				if err != nil {
					return err
				}
				fields, err := repos.Fields.ListByTable(ctx, tableID)
				if err != nil {
					return err
				}
				registry, err := store.NewPhysicalRegistry(tx, "fraud", tenantID, 1000)
				if err != nil {
					return err
				}
				columns, err := registry.Fields(ctx, tableID)
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
			var current datamodel.Field
			for _, f := range mapped {
				if f.Field.ID == fieldID {
					current = f.Field
				}
			}
			if current.ID == uuid.Nil {
				return sql.ErrNoRows
			}
			if err := service.ValidatePhysicalFieldPatch(current, input); err != nil && !remove {
				return err
			}
			if !remove && (input.Nullable == nil || *input.Nullable == current.Nullable) && (input.IsUnique == nil || *input.IsUnique == current.IsUnique) {
				result, err = p.publishFieldCatalog(ctx, tenantID, tableID, op, condition, input)
				return err
			}
			// Enum membership changes only the published catalog. Bind its
			// desired flag to the complete metadata postcondition of any real DDL.
			if input.IsEnum != nil {
				for i := range mapped {
					if mapped[i].Field.ID == fieldID {
						mapped[i].Field.IsEnum = *input.IsEnum
					}
				}
			}
			planner, err := store.NewPhysicalPlanner("fraud", tenantID, physical, mapped)
			if err != nil {
				return err
			}
			var statements []store.PhysicalStatement
			if remove {
				if current.IsUnique && current.DataType == datamodel.DataTypeString {
					statements, err = planner.DisableTextUnique(fieldID, previous.Creation.Definer)
				}
				if err == nil {
					var drop store.PhysicalStatement
					drop, err = planner.DropField(fieldID)
					statements = append(statements, drop)
				}
			} else {
				statements, err = physicalFieldPatchStatements(planner, current, input, previous.Creation.Definer)
			}
			if err != nil {
				return err
			}
			references, err := p.nativeReferences(ctx, physical, previous, statements)
			if err != nil {
				return err
			}
			plan, err = planner.BindRecordChanges(previous, statements, references)
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
			err = store.PreparePhysicalFieldChange(ctx, p.Target, func(tx *sql.Tx) error {
				var locked mysqlstore.UUID
				if err := tx.QueryRowContext(ctx, "SELECT id FROM fraud.data_model_tables WHERE tenant_id=? AND id=? AND NOT archived FOR UPDATE", tenant, mysqlstore.UUID(tableID)).Scan(&locked); err != nil {
					return err
				}
				if _, err := intents.PrepareTx(ctx, tx, store.DDLIntent{TableID: tableID, OperationID: op, RequestHash: hash, ActorKind: "api", ActorID: actor.String(), ExpectedEpoch: physical.Epoch, ExpectedVersion: &version, Shape: raw}); err != nil {
					return err
				}
				var status string
				if err := tx.QueryRowContext(ctx, "SELECT status FROM fraud.data_model_tenants WHERE id=?", tenant).Scan(&status); err != nil {
					return err
				}
				if status != "active" {
					return store.ErrModelUnavailable
				}
				if remove {
					_, err := store.CheckFieldDeletion(ctx, tx, "fraud", tenantID, tableID, fieldID)
					return err
				}
				if err := store.CheckNativeNullability(ctx, tx, "fraud", physical.Name, plan); err != nil {
					return err
				}
				// Prepare closes the writer gate in this transaction. Validate data
				// and relationship preconditions before committing durable work.
				if len(plan.EmptyTableBefore) > 0 {
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
				if input.IsUnique != nil && !*input.IsUnique && current.IsUnique {
					var id mysqlstore.UUID
					err := tx.QueryRowContext(ctx, "SELECT id FROM fraud.data_model_links WHERE tenant_id=? AND parent_table_id=? AND parent_field_id=? LIMIT 1", tenant, table, mysqlstore.UUID(fieldID)).Scan(&id)
					if err == nil {
						return service.ErrFieldDependency
					}
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
				}
				if input.IsEnum != nil && !*input.IsEnum && current.IsEnum {
					var id mysqlstore.UUID
					err := tx.QueryRowContext(ctx, "SELECT id FROM fraud.data_model_field_enum_values WHERE tenant_id=? AND field_id=? LIMIT 1", tenant, mysqlstore.UUID(fieldID)).Scan(&id)
					if err == nil {
						return service.ErrFieldDependency
					}
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
				}
				if input.IsUnique != nil && *input.IsUnique && !current.IsUnique {
					name, err := mysqlstore.Identifier(physical.Name)
					if err != nil {
						return err
					}
					var column string
					for _, f := range mapped {
						if f.Field.ID == fieldID {
							column = f.Components[0].Name
						}
					}
					quoted, err := mysqlstore.Identifier(column)
					if err != nil {
						return err
					}
					if current.DataType == datamodel.DataTypeString {
						return store.CheckTextDuplicates(ctx, tx, "fraud", physical.Name, column)
					}
					var duplicate int
					err = tx.QueryRowContext(ctx, "SELECT 1 FROM fraud."+name+" WHERE "+quoted+" IS NOT NULL GROUP BY "+quoted+" HAVING COUNT(*)>1 LIMIT 1").Scan(&duplicate)
					if err == nil {
						return service.ErrFieldDuplicate
					}
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		installer := installation.RecordInstallerPolicy{Principal: installation.Definer{User: "lcn_" + strings.ReplaceAll(tableID.String(), "-", "")[:24], Host: "localhost"}, Table: physical.Name, NativeChanges: true, TextGuards: plan.InstallsTextGuards(), BackfillColumn: plan.TextBackfillColumn()}
		if state != "published" {
			if err := p.executeNative(ctx, tenantID, tableID, op, installer); err != nil {
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
		request := ports.PhysicalPublicationRequest{TableID: tableID, OperationID: op, RequestHash: hash, Condition: condition, ActorKind: "api", ActorID: actor.String()}
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
		if remove {
			return manager.PublishFieldDeletion(ctx, request, fieldID, func(tx ports.MutationStore) error {
				return owner.ApplyPreparedDeletion(ctx, tx, service.DeleteFieldInput{FieldID: fieldID}, tenantID, tableID)
			})
		}
		result, err = owner.PublishPreparedConstraintsResult(ctx, tenantID, input, request, manager)
		return err
	})
	return result, err
}
