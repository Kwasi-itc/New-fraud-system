package mysql

import "math"

// safeFloatAccumulation implements the staged SUM/AVG conservative safety policy.
// For n<=2^52, n*unit-roundoff<=1/2. Factor-two absolute-sum headroom avoids
// intermediate overflow, including engines that return a silent zero. Extreme
// cancellation may be rejected; this is not a production parity approval.
func safeFloatAccumulation(count int64, rawMax any) bool {
	if count < 1 || count > 1<<52 {
		return false
	}
	value, err := decodeRecordSum("float", rawMax)
	if err != nil {
		return false
	}
	magnitude := value.(float64)
	return magnitude >= 0 && (count == 1 || magnitude <= math.Nextafter(math.MaxFloat64/(2*float64(count)), 0))
}
