package crema

// HitTest finds the deepest Box at the given coordinates.
func HitTest(root *Box, x, y int) *Box {
	return hitTestRecursive(root, x, y)
}

func hitTestRecursive(box *Box, x, y int) *Box {
	// Check if point is inside this box
	if x < box.X || y < box.Y || x >= box.X+box.W || y >= box.Y+box.H {
		return nil
	}

	// Check children in reverse order (last painted = on top)
	for i := len(box.Children) - 1; i >= 0; i-- {
		child := box.Children[i]
		hit := hitTestRecursive(child, x, y)
		if hit != nil {
			return hit
		}
	}

	// No child hit — return this box if it has an element or is interactive
	if box.Element != nil || box.Link != "" || box.InputType != "" {
		return box
	}

	return box
}

// HitTestElement finds the Element at the given coordinates.
// Returns the element and what action should be taken.
type HitResult struct {
	Element     *Element
	Box         *Box
	Action      string // "navigate", "click", "input", "none"
	Link        string // href for links
	InputType   string // type for inputs
	Text        string // visible text
}

func HitTestElement(root *Box, x, y int) *HitResult {
	box := HitTest(root, x, y)
	if box == nil {
		return &HitResult{Action: "none"}
	}

	result := &HitResult{
		Box:  box,
		Text: box.Text,
	}

	// Check the box itself
	if box.Link != "" {
		result.Action = "navigate"
		result.Link = box.Link
		result.Element = box.Element
		return result
	}

	if box.InputType != "" {
		result.Action = "input"
		result.InputType = box.InputType
		result.Element = box.Element
		return result
	}

	if box.Element != nil {
		result.Element = box.Element

		// Check if element is a link
		if box.Element.TagName == "A" {
			result.Action = "navigate"
			result.Link = box.Element.GetAttribute("href")
			return result
		}

		// Check if element is a button
		if box.Element.TagName == "BUTTON" {
			result.Action = "click"
			return result
		}

		// Check if element is an input
		if box.Element.TagName == "INPUT" {
			result.Action = "input"
			result.InputType = box.Element.GetAttribute("type")
			return result
		}

		// Walk up parents to find interactive ancestor
		parent := box.Element.Parent
		for parent != nil {
			pel := nodeToElement(parent)
			if pel != nil {
				if pel.TagName == "A" {
					result.Action = "navigate"
					result.Link = pel.GetAttribute("href")
					result.Element = pel
					return result
				}
				if pel.TagName == "BUTTON" {
					result.Action = "click"
					result.Element = pel
					return result
				}
			}
			parent = parent.Parent
		}
	}

	result.Action = "none"
	return result
}
