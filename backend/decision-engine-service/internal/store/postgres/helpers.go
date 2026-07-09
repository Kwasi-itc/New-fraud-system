package postgres

func nullableEmptyString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
