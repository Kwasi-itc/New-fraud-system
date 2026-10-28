package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
)

// This is deployment input, not a grant installer. Bind it to the migration
// database so a policy prepared for another installation cannot pass silently.
type accountPolicies struct {
	Format   string                       `json:"format"`
	Database string                       `json:"database"`
	Accounts []installation.AccountPolicy `json:"accounts"`
}

func readAccountPolicies(path, database string) ([]installation.AccountPolicy, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	return decodeAccountPolicies(data, database)
}

func decodeAccountPolicies(data []byte, database string) ([]installation.AccountPolicy, error) {
	if len(data) > 1<<20 {
		return nil, errors.New("account policies exceed 1 MiB")
	}
	// encoding/json normally accepts duplicate keys and case-insensitive struct
	// fields. Reject ambiguous reviewed documents before decoding their values.
	tokens := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 16 {
			return errors.New("account policy nesting exceeds limit")
		}
		token, err := tokens.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for tokens.More() {
				key, err := tokens.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok {
					return errors.New("invalid policy key")
				}
				name = strings.ToLower(name)
				if seen[name] {
					return errors.New("duplicate account policy key")
				}
				seen[name] = true
				if err = walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for tokens.More() {
				if err = walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid policy delimiter")
		}
		_, err = tokens.Token()
		return err
	}
	if err := walk(0); err != nil {
		return nil, err
	}
	if _, err := tokens.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("one account policy document required")
	}
	var document accountPolicies
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if document.Format != "dm-account-policies/v1" || document.Database != database || len(document.Accounts) == 0 || len(document.Accounts) > 64 {
		return nil, errors.New("matching database, policy format and 1..64 accounts required")
	}
	seen := map[string]bool{}
	total := 0
	for _, account := range document.Accounts {
		key := account.User + "\x00" + account.Host
		if seen[key] {
			return nil, errors.New("duplicate account policy")
		}
		seen[key] = true
		grants, err := installation.GrantStatements(database, account)
		if err != nil {
			return nil, err
		}
		for _, grant := range grants {
			total += len(grant)
		}
		if total > 1<<20 {
			return nil, errors.New("combined rendered account policies exceed 1 MiB")
		}
	}
	return document.Accounts, nil
}
