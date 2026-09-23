package domain

// ValueOrZero returns the dereferenced value of a pointer if non-nil, or the zero value of T.
func ValueOrZero[T any](v *T) T {
	if v == nil {
		var zero T
		return zero
	}
	return *v
}
