package util

// Map applies a transformation function to each element of a slice and returns a new slice
// with the transformed values. This is a generic implementation of the map higher-order function.
//
// Type Parameters:
//   - A: The type of elements in the input slice
//   - B: The type of elements in the output slice
//
// Parameters:
//   - coll: The input slice to transform
//   - mapper: Function that transforms each element and receives the element's index
//
// Returns:
//   - []B: A new slice containing the transformed elements
func Map[A any, B any](coll []A, mapper func(i A, index uint64) B) []B {
	out := make([]B, len(coll))
	for i, item := range coll {
		out[i] = mapper(item, uint64(i))
	}
	return out
}

// Find returns the first element in a slice that satisfies the provided criteria function.
// If no element satisfies the criteria, nil is returned.
//
// Type Parameters:
//   - A: The type of elements in the slice
//
// Parameters:
//   - coll: The input slice to search
//   - criteria: Function that determines whether an element matches
//
// Returns:
//   - *A: Pointer to the first matching element, or nil if no match is found
func Find[A any](coll []*A, criteria func(i *A) bool) *A {
	for _, item := range coll {
		if criteria(item) {
			return item
		}
	}
	return nil
}
