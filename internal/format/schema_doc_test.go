package format_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/CodeMonkey80s/golens/internal/format"
)

// TestJSONSchemaDocumentCoversPayloadFields makes documentation drift a test failure rather
// than something a reader discovers. Every JSON tag on a payload struct must appear in the
// schema document; adding a field without documenting it fails here.
func TestJSONSchemaDocumentCoversPayloadFields(t *testing.T) {
	doc, err := os.ReadFile("../../documentation/json-schema.md")
	if err != nil {
		t.Fatalf("read schema doc: %v", err)
	}
	text := string(doc)

	payloads := []any{
		format.BoundsPayload{},
		format.GetPayload{},
		format.OutlineFile{},
		format.OutlineNode{},
		format.ErrorPayload{},
		format.ErrorDetail{},
	}

	for _, payload := range payloads {
		typ := reflect.TypeOf(payload)
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			if tag == "" || tag == "-" {
				continue
			}
			name := strings.Split(tag, ",")[0]
			// ErrorDetail is documented under the error envelope as error.kind,
			// error.message, and so on.
			documented := strings.Contains(text, "`"+name+"`") ||
				strings.Contains(text, "`error."+name+"`")
			if !documented {
				t.Errorf("%s.%s (json %q) is missing from documentation/json-schema.md",
					typ.Name(), typ.Field(i).Name, name)
			}
		}
	}
}
