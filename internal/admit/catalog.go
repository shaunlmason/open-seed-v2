package admit

// CatalogVerbs returns every verb the affordance catalog drafts, in the
// catalog's own order — the one verb list the affordance computation and
// the generated docs both read, so a capability document cannot enumerate
// a verb the boundary does not draft, nor miss one it does
// (plans/os-16e55c11.md D1). The catalog's completeness against the
// spec table is pinned both ways by the lanes suite (specCatalogVerbs);
// this accessor exposes the same list without a second declaration.
func CatalogVerbs() []string {
	out := make([]string, 0, len(affordanceCatalog))
	for _, p := range affordanceCatalog {
		out = append(out, p.verb)
	}
	return out
}
