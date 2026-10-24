package brick

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Regions. A region is an element of
// a stateful component's own template marked brick:ref="name" brick:ignore:
// ordinary morphs leave it as it is, and only an action that names it
// changes it, by replacing its children with the ones the same element has
// in the fresh render, or by appending or prepending them. That gives a
// "load more" list or an "older messages" log written inline in the
// template, whose earlier pages live only in the browser.
//
// How it differs from its neighbours: brick.rows edits a list by key, one
// separately rendered component per row; brick:lazy renders a component
// call once, later, from a ticket. A region needs neither a row component
// nor keys, and it is driven by actions. The component still renders once
// on the server (template speed is a non-goal); what the region saves is
// the browser's copy, which keeps what earlier answers put there.
//
// The word is region, not island: in brick an island is a component
// rendered per request inside a cached page.

// EffectRegion is the region effect's kind: Ref names the region, Op is
// one of the Region* operations.
const EffectRegion = "region"

// The region operations. RegionReplace swaps the region's children for the
// fresh render's (a morph, so focus and mounted code inside survive);
// RegionAppend and RegionPrepend add the fresh children after or before
// the ones the region holds.
const (
	RegionReplace = "replace"
	RegionAppend  = "append"
	RegionPrepend = "prepend"
)

var regionOps = []string{RegionReplace, RegionAppend, RegionPrepend}

// Region records a region effect: once the action's render has been
// morphed in, the runtime applies the children of the brick:ref=ref
// element in that render to the live element, by op (RegionReplace when
// ""). The region's content comes from the render, never from the action,
// so every value in it was escaped by html/template like any other.
//
// A region needs the render, so an action that skipped it
// (brick.skip_render) cannot name one, in either order: Add refuses a
// region after skip_render and SkipRender refuses after a region.
func (e *Effects) Region(ref, op string) error {
	if e == nil {
		return errors.New("brick: effects can only be recorded inside an action")
	}
	return e.Add(Effect{Kind: EffectRegion, Ref: ref, Op: op})
}

// errRegionSkipped is the one refusal for a region and skip_render in the
// same action, whichever came first.
var errRegionSkipped = errors.New("brick: region effect: the action skipped its render (brick.skip_render), and a region is a piece of that render")

// checkRegion validates a region effect and fills its default operation;
// Effects.check calls it for EffectRegion, so Add from a host is held to
// the same rules as Region.
func checkRegion(ef *Effect) error {
	if !validRef(ef.Ref) {
		return fmt.Errorf("brick: region effect: ref %q must be a brick:ref name", ef.Ref)
	}
	if ef.Op == "" {
		ef.Op = RegionReplace
	}
	if !slices.Contains(regionOps, ef.Op) {
		return fmt.Errorf("brick: region effect: op %q is not one of %s", ef.Op, strings.Join(regionOps, ", "))
	}
	if len(ef.Rows) > 0 || len(ef.Keys) > 0 || ef.At != "" || ef.Limit != 0 {
		return errors.New("brick: region effect: a region takes its content from the render; rows, keys, at and limit belong to brick.rows")
	}
	return nil
}
