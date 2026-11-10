package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The store contract: the same scenarios run against the browser's store
// (web/src/platform/store.contract.test.ts) and against this vault. Both must
// end with the same records.
type contractFile struct {
	Scenarios []struct {
		Name   string  `json:"name"`
		Writes []Write `json:"writes"`
		Expect []struct {
			Collection Collection `json:"collection"`
			ID         string     `json:"id"`
			Value      any        `json:"value"`
		} `json:"expect"`
		Absent []struct {
			Collection Collection `json:"collection"`
			ID         string     `json:"id"`
		} `json:"absent"`
	} `json:"scenarios"`
}

func TestStoreContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "store-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract contractFile
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	for _, sc := range contract.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			v, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer v.Close()
			for _, w := range sc.Writes {
				if _, err := v.Write(w); err != nil {
					t.Fatalf("write %s %s: %v", w.Action, w.ID, err)
				}
			}
			rows := map[string]string{}
			for _, row := range v.Snapshot().Records {
				rows[string(row.Collection)+"/"+row.ID] = jsonText(t, row.Value)
			}
			for _, e := range sc.Expect {
				got, ok := rows[string(e.Collection)+"/"+e.ID]
				if !ok {
					t.Fatalf("%s/%s is missing", e.Collection, e.ID)
				}
				if want := jsonText(t, e.Value); got != want {
					t.Fatalf("%s/%s = %s, want %s", e.Collection, e.ID, got, want)
				}
			}
			for _, a := range sc.Absent {
				if _, ok := rows[string(a.Collection)+"/"+a.ID]; ok {
					t.Fatalf("%s/%s should be absent", a.Collection, a.ID)
				}
			}
		})
	}
}

// jsonText writes a value the way the other side reads it back: keys sorted,
// numbers as written.
func jsonText(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var back any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&back); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
