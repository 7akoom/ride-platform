.PHONY: help run-all run docker-build-all

help:
	@echo "Ride Platform development commands"
	@echo ""
	@echo "  make run-all             Start all 9 backend services locally (Ctrl+C to stop)"
	@echo "  make run SVC=rider-service          Start just one service"
	@echo "  make docker-build-all    Build a Docker image for every service"

run-all:
	@./scripts/run-all.sh

run:
	@./scripts/run-all.sh $(SVC)

docker-build-all:
	@./scripts/docker-build-all.sh