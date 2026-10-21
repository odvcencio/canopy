package outline

import (
	"context"
	"fmt"
	"sync/atomic"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// parse follows Canopy's parser-panic isolation while retaining cancellation.
func parse(ctx context.Context, src []byte, entry grammars.LangEntry, language *gts.Language) (tree *gts.Tree, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if tree != nil {
				tree.Release()
				tree = nil
			}
			err = fmt.Errorf("gotreesitter panic in %s parser: %v", entry.Name, recovered)
		}
	}()
	parser := gts.NewParser(language)
	var canceled uint32
	parser.SetCancellationFlag(&canceled)
	stop := context.AfterFunc(ctx, func() { atomic.StoreUint32(&canceled, 1) })
	defer stop()
	if entry.TokenSourceFactory != nil {
		if tokens := entry.TokenSourceFactory(src, language); tokens != nil {
			tree, err = parser.ParseWithTokenSource(src, tokens)
		} else {
			tree, err = parser.Parse(src)
		}
	} else {
		tree, err = parser.Parse(src)
	}
	if ctx.Err() != nil {
		return tree, ctx.Err()
	}
	return tree, err
}
