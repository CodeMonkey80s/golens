package usecase

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestCheckSchemaDocumentCoversPayloadFields is the check-payload half of the schema drift
// test: checkPayload and jsonDiagnostic are unexported, so the format package cannot see them.
func TestCheckSchemaDocumentCoversPayloadFields(t *testing.T) {
	doc, err := os.ReadFile("../../documentation/json-schema.md")
	if err != nil {
		t.Fatalf("read schema doc: %v", err)
	}
	text := string(doc)

	for _, payload := range []any{checkPayload{}, jsonDiagnostic{}} {
		typ := reflect.TypeOf(payload)
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			if tag == "" || tag == "-" {
				continue
			}
			name := strings.Split(tag, ",")[0]
			if !strings.Contains(text, "`"+name+"`") {
				t.Errorf("%s.%s (json %q) is missing from documentation/json-schema.md",
					typ.Name(), typ.Field(i).Name, name)
			}
		}
	}
}
