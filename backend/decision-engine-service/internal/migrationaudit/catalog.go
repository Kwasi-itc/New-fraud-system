// Package migrationaudit builds review artifacts, not executable MySQL SQL.
// Nothing here is selected by API/worker startup or allowed to adopt a deployed
// schema. Its catalog input comes from generated disposable PostgreSQL fixtures.
package migrationaudit

type Catalog struct {
	Tables   []Table   `json:"tables"`
	Routines []Routine `json:"routines"`
	Triggers []Trigger `json:"triggers"`
}
type Table struct {
	Name        string       `json:"name"`
	Columns     []Column     `json:"columns"`
	Constraints []Constraint `json:"constraints"`
	Indexes     []Index      `json:"indexes"`
}
type Column struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Nullable  bool    `json:"nullable"`
	Default   *string `json:"default"`
	Collation *string `json:"collation"`
}
type Constraint struct {
	Name, Kind, Definition                   string
	Columns                                  []string
	ReferencedTable                          *string
	ReferencedColumns                        []string
	Deferrable, InitiallyDeferred, Validated bool
}
type Index struct {
	Name, Definition, Method string
	Primary, Unique, Valid   bool
	Keys                     []string
	Predicate                *string
}
type Routine struct {
	Name, Signature, Definition string
	SecurityDefiner             bool
}
type Trigger struct{ Name, Table, Definition, Enabled string }

// CatalogSQL reads only core objects installed by this owner's migrations in a
// fresh fixture. Full expressions/definitions are retained: no regex parser can
// silently omit an ALTER, expression index, check, trigger or routine body.
const CatalogSQL = `SELECT jsonb_build_object(
 'tables',(SELECT coalesce(jsonb_agg(jsonb_build_object(
   'name','core.'||c.relname,
   'columns',(SELECT coalesce(jsonb_agg(jsonb_build_object(
     'name',a.attname,'type',format_type(a.atttypid,a.atttypmod),
     'nullable',NOT a.attnotnull,'default',pg_get_expr(d.adbin,d.adrelid),
     'collation',CASE WHEN a.attcollation=0 THEN NULL ELSE cn.nspname||'.'||co.collname END
     ) ORDER BY a.attnum),'[]') FROM pg_attribute a
     LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
     LEFT JOIN pg_collation co ON co.oid=a.attcollation LEFT JOIN pg_namespace cn ON cn.oid=co.collnamespace
     WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped),
   'constraints',(SELECT coalesce(jsonb_agg(jsonb_build_object(
     'Name',k.conname,'Kind',k.contype,'Definition',pg_get_constraintdef(k.oid,true),
     'Columns',ARRAY(SELECT a.attname FROM unnest(k.conkey) WITH ORDINALITY key(n,ord) JOIN pg_attribute a ON a.attrelid=k.conrelid AND a.attnum=key.n ORDER BY key.ord),
     'ReferencedTable',CASE WHEN k.confrelid=0 THEN NULL ELSE rn.nspname||'.'||rc.relname END,
     'ReferencedColumns',ARRAY(SELECT a.attname FROM unnest(k.confkey) WITH ORDINALITY key(n,ord) JOIN pg_attribute a ON a.attrelid=k.confrelid AND a.attnum=key.n ORDER BY key.ord),
     'Deferrable',k.condeferrable,'InitiallyDeferred',k.condeferred,'Validated',k.convalidated
     ) ORDER BY k.conname),'[]') FROM pg_constraint k LEFT JOIN pg_class rc ON rc.oid=k.confrelid LEFT JOIN pg_namespace rn ON rn.oid=rc.relnamespace WHERE k.conrelid=c.oid),
   'indexes',(SELECT coalesce(jsonb_agg(jsonb_build_object(
     'Name',ic.relname,'Definition',pg_get_indexdef(i.indexrelid),'Method',am.amname,
     'Primary',i.indisprimary,'Unique',i.indisunique,'Valid',i.indisvalid,
     'Keys',ARRAY(SELECT pg_get_indexdef(i.indexrelid,pos,true) FROM generate_series(1,i.indnkeyatts) pos),
     'Predicate',pg_get_expr(i.indpred,i.indrelid)
     ) ORDER BY ic.relname),'[]') FROM pg_index i JOIN pg_class ic ON ic.oid=i.indexrelid JOIN pg_am am ON am.oid=ic.relam WHERE i.indrelid=c.oid)
   ) ORDER BY c.relname),'[]') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='core' AND c.relkind='r'),
 'routines',(SELECT coalesce(jsonb_agg(jsonb_build_object('Name','core.'||p.proname,'Signature',pg_get_function_identity_arguments(p.oid),'Definition',pg_get_functiondef(p.oid),'SecurityDefiner',p.prosecdef) ORDER BY p.proname,pg_get_function_identity_arguments(p.oid)),'[]') FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='core' AND p.prokind='f'),
 'triggers',(SELECT coalesce(jsonb_agg(jsonb_build_object('Name',t.tgname,'Table','core.'||c.relname,'Definition',pg_get_triggerdef(t.oid,true),'Enabled',t.tgenabled) ORDER BY c.relname,t.tgname),'[]') FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='core' AND NOT t.tgisinternal)
)`
