package changeintel

import (
	"sort"

	"m31labs.dev/canopy/pkg/model"
)

// nameKey groups entity builders that share a file/kind/name but not
// necessarily a receiver — used to detect receiver/type changes, which by
// definition change the primary entityKey and would otherwise show up as an
// unrelated add + remove pair.
type nameKey struct {
	file, kind, name string
}

func nameKeyLess(a, b nameKey) bool {
	if a.file != b.file {
		return a.file < b.file
	}
	if a.kind != b.kind {
		return a.kind < b.kind
	}
	return a.name < b.name
}

func groupByFileKindName(list []*entityBuilder, sym func(*entityBuilder) *model.Symbol) map[nameKey][]*entityBuilder {
	groups := map[nameKey][]*entityBuilder{}
	for _, b := range list {
		s := sym(b)
		if s == nil {
			continue
		}
		k := nameKey{file: b.key.file, kind: s.Kind, name: s.Name}
		groups[k] = append(groups[k], b)
	}
	return groups
}

// reconcileAPIDelta finishes building the public API delta: it merges the
// signature-changed entries already found while matching entities with
// added/removed entries derived from baseOnly/headOnly, first checking
// whether an apparent add+remove pair is actually the same exported symbol
// with a changed receiver (a type it moved to, or a type parameter change
// that altered the receiver text).
func reconcileAPIDelta(baseOnly, headOnly []*entityBuilder, apiChanged []APISymbolChange, fileLang map[string]string) APIDelta {
	reconciledBase := map[*entityBuilder]bool{}
	reconciledHead := map[*entityBuilder]bool{}

	baseGroups := groupByFileKindName(baseOnly, func(b *entityBuilder) *model.Symbol { return b.baseSym })
	headGroups := groupByFileKindName(headOnly, func(b *entityBuilder) *model.Symbol { return b.headSym })

	var keys []nameKey
	for k := range baseGroups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return nameKeyLess(keys[i], keys[j]) })

	for _, k := range keys {
		bg := baseGroups[k]
		hg, ok := headGroups[k]
		if !ok || len(bg) != 1 || len(hg) != 1 {
			continue // ambiguous or one-sided: leave as plain add/remove candidates
		}
		b, h := bg[0], hg[0]
		if b.baseSym.Receiver == h.headSym.Receiver {
			continue // same receiver: not a receiver change, some other mismatch
		}
		lang := fileLang[k.file]
		if !isExported(k.name, lang) {
			continue
		}
		reconciledBase[b] = true
		reconciledHead[h] = true
		apiChanged = append(apiChanged, APISymbolChange{
			File: k.file, Kind: k.kind, Name: k.name, Receiver: h.headSym.Receiver,
			Change:         APIReceiverChanged,
			BaseSignature:  b.baseSym.Signature,
			HeadSignature:  h.headSym.Signature,
			BaseVisibility: visibilityLabel(k.name, lang),
			HeadVisibility: visibilityLabel(k.name, lang),
		})
	}

	var delta APIDelta
	delta.Changed = apiChanged

	for _, b := range baseOnly {
		if reconciledBase[b] {
			continue
		}
		lang := fileLang[b.key.file]
		if !isExported(b.baseSym.Name, lang) {
			continue
		}
		delta.Removed = append(delta.Removed, APISymbolChange{
			File: b.key.file, Kind: b.key.kind, Name: b.key.name, Receiver: b.key.receiver,
			Change:         APIRemoved,
			BaseSignature:  b.baseSym.Signature,
			BaseVisibility: visibilityLabel(b.baseSym.Name, lang),
		})
	}
	for _, h := range headOnly {
		if reconciledHead[h] {
			continue
		}
		lang := fileLang[h.key.file]
		if !isExported(h.headSym.Name, lang) {
			continue
		}
		delta.Added = append(delta.Added, APISymbolChange{
			File: h.key.file, Kind: h.key.kind, Name: h.key.name, Receiver: h.key.receiver,
			Change:         APIAdded,
			HeadSignature:  h.headSym.Signature,
			HeadVisibility: visibilityLabel(h.headSym.Name, lang),
		})
	}

	sortAPIChanges(delta.Added)
	sortAPIChanges(delta.Removed)
	sortAPIChanges(delta.Changed)
	return delta
}

func sortAPIChanges(items []APISymbolChange) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].File != items[j].File {
			return items[i].File < items[j].File
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Receiver < items[j].Receiver
	})
}
