package sync

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"notes/internal/vault"
)

// MergePage merges a page file that changed on both sides. base is the page
// both sides last saved (nil when there is none), local and remote are the
// two versions. Records and fields merge the way the web app's notebook
// merges them: a value only one side changed is taken from that side, and a
// value both sides changed keeps the local one. fields names the values both
// sides changed, which is what a conflict is.
func MergePage(base, local, remote []byte) (merged []byte, fields []string, err error) {
	b := state{value: vault.Record{}, present: true}
	if len(bytes.TrimSpace(base)) > 0 {
		obj, err := vault.DecodeObject(base)
		if err != nil {
			return nil, nil, fmt.Errorf("sync: base page: %w", err)
		}
		b = state{value: obj, present: true}
	}
	l, err := vault.DecodeObject(local)
	if err != nil {
		return nil, nil, fmt.Errorf("sync: local page: %w", err)
	}
	r, err := vault.DecodeObject(remote)
	if err != nil {
		return nil, nil, fmt.Errorf("sync: remote page: %w", err)
	}
	conflicts := []string{}
	out := mergeNode("", b, state{value: l, present: true}, state{value: r, present: true}, &conflicts)
	obj, ok := out.value.(vault.Record)
	if !ok {
		return nil, nil, errors.New("sync: merged page is not an object")
	}
	data, err := vault.EncodeObject(obj)
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(conflicts)
	return data, conflicts, nil
}

// state is a value that may be absent: a missing field, or a missing record.
type state struct {
	value   any
	present bool
}

func stateOf(m map[string]any, key string) state {
	if m == nil {
		return state{}
	}
	v, ok := m[key]
	return state{value: v, present: ok}
}

func sameState(a, b state) bool {
	return a.present == b.present && (!a.present || reflect.DeepEqual(a.value, b.value))
}

// mergeNode merges one value of the three versions. A value that only one
// side changed is taken from that side. Two objects changed on both sides
// merge field by field. Anything else changed on both sides is a conflict:
// the local value stays, and path names it.
func mergeNode(path string, base, local, remote state, conflicts *[]string) state {
	if sameState(local, remote) {
		return local
	}
	if sameState(local, base) {
		return remote
	}
	if sameState(remote, base) {
		return local
	}
	lm, lok := local.value.(map[string]any)
	rm, rok := remote.value.(map[string]any)
	if local.present && remote.present && lok && rok {
		bm, _ := base.value.(map[string]any)
		if !base.present {
			bm = nil
		}
		keys := map[string]bool{}
		for k := range lm {
			keys[k] = true
		}
		for k := range rm {
			keys[k] = true
		}
		for k := range bm {
			keys[k] = true
		}
		out := map[string]any{}
		for _, key := range sortedKeys(keys) {
			child := mergeNode(join(path, key), stateOf(bm, key), stateOf(lm, key), stateOf(rm, key), conflicts)
			if child.present {
				out[key] = child.value
			}
		}
		return state{value: vault.Record(out), present: true}
	}
	if path != "" {
		*conflicts = append(*conflicts, path)
	}
	return local
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
