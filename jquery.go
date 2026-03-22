package crema

import (
	"strings"

	"github.com/marcoschwartz/espresso"
)

// injectjQuery registers $ and jQuery as native Go functions in the VM.
// This approach is more reliable than running a JS shim because it doesn't
// depend on espresso supporting constructor patterns or prototype chains.
func injectjQuery(vm *espresso.VM, page *Page) {
	jqFunc := espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 {
			return newjQuerySet(nil, page)
		}
		arg := args[0]

		// $(function) → document.ready (call immediately, page is loaded)
		if arg.Type() == espresso.TypeFunc {
			espresso.CallFuncValue(arg, nil, vm.Scope())
			return espresso.Undefined
		}

		// $(string) → selector query
		if arg.Type() == espresso.TypeString {
			sel := arg.String()
			// $("<html>") → skip HTML creation
			if len(sel) > 0 && sel[0] == '<' {
				return newjQuerySet(nil, page)
			}
			if page.Doc == nil {
				return newjQuerySet(nil, page)
			}
			elements := QuerySelectorAll(&page.Doc.Node, sel)
			return newjQuerySet(elements, page)
		}

		// $(element) → wrap DOM element
		// Just return a jQuery set for it
		return newjQuerySet(nil, page)
	})

	vm.SetValue("jQuery", jqFunc)
	vm.SetValue("$", jqFunc)

	// NativeFunc has no object map by default — skip static methods for now
	// Sites that use $.ajax etc. will get undefined which won't crash
	return
	// Dead code below — static methods would go here if NativeFunc had an object map
	jqFunc.Object()["ajax"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return espresso.Undefined
	})
	jqFunc.Object()["get"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return espresso.Undefined
	})
	jqFunc.Object()["post"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return espresso.Undefined
	})
	jqFunc.Object()["getJSON"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return espresso.Undefined
	})
	jqFunc.Object()["each"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return espresso.Undefined
	})
	jqFunc.Object()["extend"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 { return args[0] }
		return espresso.NewObj(map[string]*espresso.Value{})
	})
	jqFunc.Object()["noop"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return espresso.Undefined
	})
	jqFunc.Object()["fn"] = espresso.NewObj(map[string]*espresso.Value{})
}

// newjQuerySet creates a jQuery-like wrapper around a set of elements.
func newjQuerySet(elements []*Element, page *Page) *espresso.Value {
	obj := espresso.NewObj(map[string]*espresso.Value{
		"length": espresso.NewNum(float64(len(elements))),
	})

	// Index access
	for i, el := range elements {
		obj.Object()[string(rune('0'+i))] = elementToJS(el)
	}

	self := obj // for chaining

	// .each(fn)
	obj.Object()["each"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return self
	})

	// .html(val) / .html()
	obj.Object()["html"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 {
			if len(elements) > 0 { return espresso.NewStr(elements[0].InnerHTML()) }
			return espresso.NewStr("")
		}
		for _, el := range elements {
			el.Children = nil
			tn := NewTextNode(args[0].String())
			RegisterNode(&tn.Node, tn)
			el.AppendChild(&tn.Node)
		}
		return self
	})

	// .text(val) / .text()
	obj.Object()["text"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 {
			if len(elements) > 0 {
				var sb strings.Builder
				CollectTextFromElement(elements[0], &sb)
				return espresso.NewStr(sb.String())
			}
			return espresso.NewStr("")
		}
		for _, el := range elements {
			el.Children = nil
			tn := NewTextNode(args[0].String())
			RegisterNode(&tn.Node, tn)
			el.AppendChild(&tn.Node)
		}
		return self
	})

	// .val(val) / .val()
	obj.Object()["val"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 {
			if len(elements) > 0 { return espresso.NewStr(elements[0].GetAttribute("value")) }
			return espresso.NewStr("")
		}
		for _, el := range elements { el.SetAttribute("value", args[0].String()) }
		return self
	})

	// .attr(name, val)
	obj.Object()["attr"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.Null }
		if len(args) == 1 {
			if len(elements) > 0 { return espresso.NewStr(elements[0].GetAttribute(args[0].String())) }
			return espresso.Null
		}
		for _, el := range elements { el.SetAttribute(args[0].String(), args[1].String()) }
		return self
	})

	// .data(name, val)
	obj.Object()["data"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.Null }
		key := "data-" + args[0].String()
		if len(args) == 1 {
			if len(elements) > 0 { return espresso.NewStr(elements[0].GetAttribute(key)) }
			return espresso.Null
		}
		for _, el := range elements { el.SetAttribute(key, args[1].String()) }
		return self
	})

	// .addClass / .removeClass / .toggleClass / .hasClass
	obj.Object()["addClass"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 {
			cls := args[0].String()
			for _, el := range elements {
				if !containsClass(el.ClassList, cls) { el.ClassList = append(el.ClassList, cls); el.Attrs["class"] = strings.Join(el.ClassList, " ") }
			}
		}
		return self
	})
	obj.Object()["removeClass"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 {
			cls := args[0].String()
			for _, el := range elements {
				for i, c := range el.ClassList { if c == cls { el.ClassList = append(el.ClassList[:i], el.ClassList[i+1:]...); break } }
				el.Attrs["class"] = strings.Join(el.ClassList, " ")
			}
		}
		return self
	})
	obj.Object()["hasClass"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 && len(elements) > 0 { return espresso.NewBool(containsClass(elements[0].ClassList, args[0].String())) }
		return espresso.NewBool(false)
	})
	obj.Object()["toggleClass"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 {
			cls := args[0].String()
			for _, el := range elements {
				if containsClass(el.ClassList, cls) {
					for i, c := range el.ClassList { if c == cls { el.ClassList = append(el.ClassList[:i], el.ClassList[i+1:]...); break } }
				} else {
					el.ClassList = append(el.ClassList, cls)
				}
				el.Attrs["class"] = strings.Join(el.ClassList, " ")
			}
		}
		return self
	})

	// .css(prop, val)
	obj.Object()["css"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return self
	})

	// .show() / .hide() / .toggle()
	obj.Object()["show"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["hide"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["toggle"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })

	// .on(event, fn) / .off() / .click()
	obj.Object()["on"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["off"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["click"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["submit"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["trigger"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })

	// .find(sel)
	obj.Object()["find"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return newjQuerySet(nil, page) }
		var results []*Element
		for _, el := range elements {
			found := QuerySelectorAll(&el.Node, args[0].String())
			results = append(results, found...)
		}
		return newjQuerySet(results, page)
	})

	// .parent() / .children() / .siblings() / .first() / .last() / .eq()
	obj.Object()["parent"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return newjQuerySet(nil, page) })
	obj.Object()["children"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return newjQuerySet(nil, page) })
	obj.Object()["siblings"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return newjQuerySet(nil, page) })
	obj.Object()["first"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(elements) > 0 { return newjQuerySet(elements[:1], page) }
		return newjQuerySet(nil, page)
	})
	obj.Object()["last"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(elements) > 0 { return newjQuerySet(elements[len(elements)-1:], page) }
		return newjQuerySet(nil, page)
	})
	obj.Object()["eq"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 {
			idx := int(args[0].Number())
			if idx >= 0 && idx < len(elements) { return newjQuerySet([]*Element{elements[idx]}, page) }
		}
		return newjQuerySet(nil, page)
	})

	// .append / .prepend / .remove / .empty
	obj.Object()["append"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["prepend"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["remove"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["empty"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		for _, el := range elements { el.Children = nil }
		return self
	})

	// .is(sel) / .filter(sel) / .not(sel)
	obj.Object()["is"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return espresso.NewBool(false) })
	obj.Object()["filter"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["not"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })

	// Animation stubs
	obj.Object()["animate"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["fadeIn"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["fadeOut"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["slideDown"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["slideUp"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["slideToggle"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })

	// Dimension stubs
	obj.Object()["width"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return espresso.NewNum(0) })
	obj.Object()["height"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return espresso.NewNum(0) })
	obj.Object()["offset"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return espresso.NewObj(map[string]*espresso.Value{"top": espresso.NewNum(0), "left": espresso.NewNum(0)}) })
	obj.Object()["scrollTop"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return espresso.NewNum(0) })

	// .ready(fn)
	obj.Object()["ready"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 && args[0].Type() == espresso.TypeFunc {
			espresso.CallFuncValue(args[0], nil, nil)
		}
		return self
	})

	// .map / .get
	obj.Object()["map"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return espresso.NewArr(nil) })
	obj.Object()["get"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return espresso.NewArr(nil) })

	// .prop / .removeAttr
	obj.Object()["prop"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })
	obj.Object()["removeAttr"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })

	// Deferred stub
	obj.Object()["promise"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value { return self })

	return obj
}

func containsClass(list []string, cls string) bool {
	for _, c := range list {
		if c == cls { return true }
	}
	return false
}
