package postgres

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UnitOfWork struct{ db *pgxpool.Pool }

func NewUnitOfWork(db *pgxpool.Pool) UnitOfWork { return UnitOfWork{db: db} }

func NewRepositories(db queryable) ports.Repositories {
	return ports.Repositories{
		Selections: DecisionSelectionRepository{db: db},
		Explorer:   ExplorationRepository{db: db},
		Analytics:  AnalyticsRepository{db: db},
		Reports:    ReportRepository{db: db},
		Uploads:    UploadRepository{db: db},
		Workspace:  WorkspaceRepository{db: db},
		Intake:     IntakeRepository{db: db},
		Inboxes:    NewInboxRepository(db), Cases: NewCaseRepository(db),
		Decisions: NewDecisionLinkRepository(db), Screenings: NewScreeningLinkRepository(db),
		Tags: NewTagRepository(db), Events: NewEventRepository(db), Files: NewFileRepository(db),
		Memberships: MembershipRepository{db: db}, Contributors: ContributorRepository{db: db}, References: ReferenceRepository{db: db},
	}
}

func (u UnitOfWork) WithinTransaction(ctx context.Context, fn func(ports.Repositories) error) error {
	tx, err := u.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		// Request cancellation must not prevent releasing a transaction's connection/locks.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	// Read transactions still enforce owner/inbox rules, but a declared read
	// policy uses the verified token snapshot. Trusted non-HTTP callers without
	// route metadata retain the conservative current-authority check.
	capability := identity.RequiredCapability(ctx)
	currentAuthority := capability == "" || identity.RequiresCurrentAuthority(capability)
	if p, ok := access.FromContext(ctx); ok && p.Kind == access.User && currentAuthority {
		if err := identity.CheckIdentity(ctx, tx, p.Platform(), true); err != nil {
			var denied *identity.Error
			if !errors.As(err, &denied) || denied.Code != 403 {
				return err
			}
			return casepkg.ErrForbidden
		}
	}
	repos := NewRepositories(tx)
	if err := fn(repos); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
