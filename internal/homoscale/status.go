package homoscale

type ServiceState string

const (
	StateOn  ServiceState = "on"
	StateOff ServiceState = "off"
)

type StatusSnapshot struct {
	Overall ServiceState `json:"overall"`
	Auth    ServiceState `json:"auth"`
	Engine  ServiceState `json:"engine"`
}

func ReadStatus(cfg *Config) *StatusSnapshot {
	snapshot := &StatusSnapshot{
		Overall: StateOff,
		Auth:    StateOff,
		Engine:  StateOff,
	}
	if auth, err := ReadAuthStatus(cfg); err == nil {
		snapshot.Auth = statusFromAuth(auth)
	}
	if engine := ReadEngineStatus(cfg); engine != nil {
		snapshot.Engine = statusFromEngine(engine)
	}
	if snapshot.Auth == StateOn && snapshot.Engine == StateOn {
		snapshot.Overall = StateOn
	}
	return snapshot
}

func statusFromAuth(status *AuthStatus) ServiceState {
	if status != nil && status.LoggedIn {
		return StateOn
	}
	return StateOff
}

func statusFromEngine(status *EngineStatus) ServiceState {
	if status != nil && status.Reachable {
		return StateOn
	}
	return StateOff
}
