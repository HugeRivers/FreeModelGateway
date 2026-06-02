package model

type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusCooldown  Status = "cooldown"
	StatusExhausted Status = "exhausted"
)
