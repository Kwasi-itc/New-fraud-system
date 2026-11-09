package ast_eval

import (
	"context"
	"errors"
	"testing"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
)

type listFailureReader struct {
	countingCustomListRepo
	failure                         error
	getCalls, nameCalls, entryCalls int
}

func (r *listFailureReader) GetListByID(_ context.Context, tenant, id string) (platform.CustomList, error) {
	r.getCalls++
	return platform.CustomList{ID: id, TenantID: tenant}, r.failure
}
func (r *listFailureReader) ListByName(context.Context, string, string) ([]platform.CustomListEntry, error) {
	r.nameCalls++
	return []platform.CustomListEntry{{Value: "legacy"}}, nil
}
func (r *listFailureReader) ListEntriesByListID(context.Context, string, string) ([]platform.CustomListEntry, error) {
	r.entryCalls++
	return []platform.CustomListEntry{{Value: "by-id"}}, nil
}

func TestCustomListAccessPropagatesLookupFailures(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("authorization revoked"), errors.New("connection lost"), errors.New("projection too large")} {
		t.Run(failure.Error(), func(t *testing.T) {
			repo := &listFailureReader{failure: failure}
			node := domainast.Node{Function: "CustomListAccess", NamedChildren: map[string]domainast.Node{"customListId": {Constant: uuid.NewString()}}}
			got, err := EvaluateNode(context.Background(), node, Runtime{TenantID: uuid.NewString(), CustomListRepo: repo})
			if !errors.Is(err, failure) || got != nil || repo.getCalls != 1 || repo.nameCalls != 0 || repo.entryCalls != 0 {
				t.Fatal("lookup failure became a successful fallback", got, err, repo)
			}
		})
	}
}

func TestCustomListAccessPreservesIDAndLegacyNames(t *testing.T) {
	for _, tc := range []struct {
		name, ref, want      string
		failure              error
		gets, names, entries int
	}{
		{"id", uuid.NewString(), "by-id", nil, 1, 0, 1},
		{"legacy name", "Watch List", "legacy", nil, 0, 1, 0},
		{"uuid-shaped name", uuid.NewString(), "legacy", errors.Join(errors.New("wrapped"), ports.ErrCustomListNotFound), 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &listFailureReader{failure: tc.failure}
			node := domainast.Node{Function: "CustomListAccess", NamedChildren: map[string]domainast.Node{"customListId": {Constant: tc.ref}}}
			got, err := EvaluateNode(context.Background(), node, Runtime{TenantID: uuid.NewString(), CustomListRepo: repo})
			if err != nil {
				t.Fatal(err)
			}
			values, ok := got.([]any)
			if !ok || len(values) != 1 || values[0] != tc.want || repo.getCalls != tc.gets || repo.nameCalls != tc.names || repo.entryCalls != tc.entries {
				t.Fatal(got, repo)
			}
		})
	}
}
