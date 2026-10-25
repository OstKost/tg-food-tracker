.PHONY: install quality test run setup

install:
	python3.14 -m pip install -r requirements.lock
	python3.14 -m pip install --no-deps -e .

quality:
	python3.14 -m ruff check .
	python3.14 -m mypy src tests
	python3.14 -m pytest

test:
	python3.14 -m pytest

run:
	python3.14 -m food_tracker

setup:
	python3.14 -m food_tracker.cli
