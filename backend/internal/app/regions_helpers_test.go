package app

import (
	"mihanstore/internal/regions"
	"mihanstore/internal/regions/regionstest"
)

// regionItems: item server tiruan (regionstest, bentuk sama) -> regions.Item.
func regionItems(in []regionstest.Item) []regions.Item {
	if in == nil {
		return nil
	}
	out := make([]regions.Item, len(in))
	for i, x := range in {
		out[i] = regions.Item(x)
	}
	return out
}
