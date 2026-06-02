package stats

func SuccessRate(success, total int64) float64 {
	if total == 0 {
		return 1.0
	}
	return float64(success) / float64(total)
}
