// Code generated by "stringer -type=suggest"; DO NOT EDIT.

package main

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[never-0]
	_ = x[once-1]
	_ = x[always-2]
}

const _suggest_name = "neveroncealways"

var _suggest_index = [...]uint8{0, 5, 9, 15}

func (i suggest) String() string {
	if i < 0 || i >= suggest(len(_suggest_index)-1) {
		return "suggest(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _suggest_name[_suggest_index[i]:_suggest_index[i+1]]
}
