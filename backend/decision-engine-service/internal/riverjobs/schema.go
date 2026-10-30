package riverjobs

// Schema is shared by the API, workers and migrator. An ambient search_path
// must not select a different queue authority in the shared logical database.
const Schema = "public"
