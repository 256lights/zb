// Code generated by "stringer -type=ComparisonOperator -linecomment -output=lua_string.go"; DO NOT EDIT.

package mylua

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Equal-0]
	_ = x[Less-1]
	_ = x[LessOrEqual-2]
}

const _ComparisonOperator_name = "==<<="

var _ComparisonOperator_index = [...]uint8{0, 2, 3, 5}

func (i ComparisonOperator) String() string {
	if i < 0 || i >= ComparisonOperator(len(_ComparisonOperator_index)-1) {
		return "ComparisonOperator(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _ComparisonOperator_name[_ComparisonOperator_index[i]:_ComparisonOperator_index[i+1]]
}
