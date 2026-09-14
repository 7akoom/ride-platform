package driver

type service struct {
	repository  Repository
	idGenerator IDGenerator
}

func NewService(
	repository Repository,
	idGenerator IDGenerator,
) Service {
	if repository == nil {
		panic("driver repository is required")
	}

	if idGenerator == nil {
		panic("driver id generator is required")
	}

	return &service{
		repository:  repository,
		idGenerator: idGenerator,
	}
}
