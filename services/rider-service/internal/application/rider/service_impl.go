package rider

type service struct {
	repository  Repository
	idGenerator IDGenerator
}

func NewService(
	repository Repository,
	idGenerator IDGenerator,
) Service {
	if repository == nil {
		panic("rider repository is required")
	}

	if idGenerator == nil {
		panic("rider id generator is required")
	}

	return &service{
		repository:  repository,
		idGenerator: idGenerator,
	}
}
