package mysql

import (
	"errors"
	"net/netip"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

var ErrRecordMembership = errors.New("mapped normalized IN predicate requires 1..100 list items")

// MembershipPredicate belongs to its originating projection and transaction.
// It preserves list order, duplicates and NULL; SQL returns detached arguments.
type MembershipPredicate struct {
	clause string
	args   []any
}

func (p MembershipPredicate) SQL() (string, []any) {
	return p.clause, cloneRecordArguments(p.args)
}

// MembershipPredicate accepts concrete []any, as the source aggregate repository
// does, and enforces the domain list limit before iteration or allocation.
// Normalized scalar values include full-byte, case-sensitive string membership.
// IP membership compares address, family and prefix together. No NOT IN extension
// or empty-list rewrite is introduced. Use with this projection's table while
// retaining its authorized READ COMMITTED transaction and publication barrier.
func (p *RecordProjection) MembershipPredicate(name string, value any) (MembershipPredicate, error) {
	items, ok := value.([]any)
	if !ok || len(items) < 1 || len(items) > ingestion.MaxAggregateInListItems || p == nil || p.table == "" {
		return MembershipPredicate{}, ErrRecordMembership
	}
	name = strings.TrimSpace(name)
	for _, field := range p.fields {
		if field.definition.Name != name {
			continue
		}
		if field.definition.Archived || field.column == "" {
			break
		}
		if field.definition.DataType == "ip_address" {
			if field.width != 3 || len(field.components) != 3 {
				return MembershipPredicate{}, ErrRecordMembership
			}
			args := make([]any, 0, 3*len(items))
			for _, item := range items {
				if item == nil {
					args = append(args, nil, nil, nil)
					continue
				}
				prefix, ok := item.(netip.Prefix)
				if !ok {
					return MembershipPredicate{}, ErrRecordMembership
				}
				value, err := mysqlstore.EncodeInet(prefix)
				if err != nil {
					return MembershipPredicate{}, ErrRecordMembership
				}
				args = append(args, value.Address, value.Family, value.PrefixBits)
			}
			return MembershipPredicate{clause: "(" + strings.Join(field.components, ",") + ") IN (" + strings.TrimSuffix(strings.Repeat("(?,?,?),", len(items)), ",") + ")", args: args}, nil
		}
		if field.width != 1 {
			return MembershipPredicate{}, ErrRecordMembership
		}
		switch field.definition.DataType {
		case "int", "float", "bool", "timestamp", "string":
		default:
			return MembershipPredicate{}, ErrRecordMembership
		}
		args := make([]any, len(items))
		for i, item := range items {
			// NULL participates in SQL three-valued logic, even on required fields.
			if item == nil {
				continue
			}
			_, encoded, err := encodeWriteValue(field.definition, item)
			if err != nil || len(encoded) != 1 {
				return MembershipPredicate{}, ErrRecordMembership
			}
			args[i] = encoded[0]
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(items)), ",")
		column := field.column
		if field.definition.DataType == "string" {
			column = "CAST(" + column + " AS BINARY)"
		}
		return MembershipPredicate{clause: column + " IN (" + placeholders + ")", args: args}, nil
	}
	return MembershipPredicate{}, ErrRecordMembership
}
