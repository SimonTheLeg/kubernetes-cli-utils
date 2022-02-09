// Code generated by "stringer -type=IDMatchStatus"; DO NOT EDIT.

package inventory

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Empty-0]
	_ = x[Match-1]
	_ = x[NoMatch-2]
}

const _IDMatchStatus_name = "EmptyMatchNoMatch"

var _IDMatchStatus_index = [...]uint8{0, 5, 10, 17}

func (i IDMatchStatus) String() string {
	if i < 0 || i >= IDMatchStatus(len(_IDMatchStatus_index)-1) {
		return "IDMatchStatus(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _IDMatchStatus_name[_IDMatchStatus_index[i]:_IDMatchStatus_index[i+1]]
}
