package investigator

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func Serve(w http.ResponseWriter, r *http.Request, v *Verifier, run func(Principal, string, *Command) (State, error)) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, message string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
	}
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		fail(401, "Investigator sign-in is required.")
		return
	}
	p, err := v.Verify(raw)
	if err != nil {
		fail(401, "Invalid investigator session.")
		return
	}
	resource := r.URL.Query().Get("resource_id")
	var command *Command
	if r.Method == http.MethodPost {
		command = &Command{}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(command); err != nil {
			fail(400, "Invalid review command.")
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			fail(400, "Invalid review command.")
			return
		}
		resource = command.ResourceID
	}
	out, err := run(p, resource, command)
	if err != nil {
		code, message := HTTPError(err)
		fail(code, message)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
