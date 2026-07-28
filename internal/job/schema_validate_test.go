package job

import (
	"testing"
)

func TestValidateSchemaBytesAcceptsValidYAML(t *testing.T) {
	data := []byte(`version: 1
inputs:
  input:
    id: 1cbs
scene:
  structures:
    - source: input
      components:
        - select: polymer
outputs:
  - type: image
    path: out.png
`)
	if err := ValidateSchemaBytes(data, "job.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSchemaBytesRejectsUnknownField(t *testing.T) {
	data := []byte(`{
  "version": 1,
  "unexpected": true,
  "inputs": {
    "input": { "id": "1cbs" }
  },
  "scene": {
    "structures": [
      { "source": "input", "components": [{ "select": "polymer" }] }
    ]
  },
  "outputs": [
    { "type": "image", "path": "out.png" }
  ]
}`)
	if err := ValidateSchemaBytes(data, "job.json"); err == nil {
		t.Fatal("expected schema validation error")
	}
}
