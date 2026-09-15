.PHONY: help run-all

help:
	@echo "Ride Platform development commands"
	@echo ""
	@echo "  make run-all   Start all 9 backend services locally (Ctrl+C to stop)"
	@echo "  make run SVC=rider-service   Start just one service"

run-all:
	@./scripts/run-all.sh

run:
	@./scripts/run-all.sh $(SVC)