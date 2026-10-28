package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestConditionalModelRepresentation(t *testing.T) {
	value := gin.H{"revision_id": "same-revision", "writable": true, "enum_values": []string{"open"}}
	request := func(condition string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/model", nil)
		ctx.Request.Header.Set("If-None-Match", condition)
		writeConditionalModel(ctx, value)
		ctx.Writer.WriteHeaderNow()
		return rec
	}
	first := request("")
	etag := first.Header().Get("ETag")
	if first.Code != 200 || etag == "" || first.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("unexpected response: %+v", first)
	}
	for _, condition := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		rec := request(condition)
		if rec.Code != 304 || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag {
			t.Fatalf("condition %s: %d %s", condition, rec.Code, rec.Body.String())
		}
	}
	if rec := request(`"different"`); rec.Code != 200 {
		t.Fatal("mismatched validator must return model")
	}
	// Invalidation must not depend solely on the migration-derived revision.
	value["writable"] = false
	if rec := request(etag); rec.Code != 200 || rec.Header().Get("ETag") == etag {
		t.Fatal("tenant status change must invalidate")
	}
	value["writable"] = true
	value["enum_values"] = []string{"closed"}
	if rec := request(etag); rec.Code != 200 || rec.Header().Get("ETag") == etag {
		t.Fatal("enum change must invalidate")
	}
}

func TestConditionalModelEncodingFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/model", nil)
	writeConditionalModel(ctx, make(chan int))
	if rec.Code != 500 || rec.Header().Get("ETag") != "" {
		t.Fatal("encoding failure must not produce a validator")
	}
}
