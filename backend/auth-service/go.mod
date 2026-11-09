module github.com/Kwasi-itc/New-fraud-system/backend/auth-service

go 1.25.0

require github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore v0.0.0

replace github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore => ../mysqlstore

require (
	github.com/coreos/go-oidc/v3 v3.16.0
	github.com/go-jose/go-jose/v4 v4.1.3
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.6
	golang.org/x/oauth2 v0.28.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/go-sql-driver/mysql v1.9.3 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)
