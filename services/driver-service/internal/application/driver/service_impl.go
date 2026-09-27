package driver

type service struct {
	repository  Repository
	idGenerator IDGenerator
	compliance  ComplianceChecker
}

func NewService(
	repository Repository,
	idGenerator IDGenerator,
	compliance ComplianceChecker,
) Service {
	if repository == nil {
		panic("driver repository is required")
	}

	if idGenerator == nil {
		panic("driver id generator is required")
	}

	if compliance == nil {
		panic("driver document compliance checker is required")
	}

	return &service{
		repository:  repository,
		idGenerator: idGenerator,
		compliance:  compliance,
	}
}
