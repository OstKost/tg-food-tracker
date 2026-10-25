FROM python:3.14.7-slim AS builder

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /build
COPY requirements.lock ./
RUN python -m pip install -r requirements.lock
COPY pyproject.toml README.md ./
COPY src ./src
RUN python -m pip wheel --no-deps --no-build-isolation --wheel-dir /wheels .

FROM python:3.14.7-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

RUN useradd --create-home --uid 10001 appuser
WORKDIR /app
COPY --from=builder /build/requirements.lock /requirements.lock
RUN python -m pip install -r /requirements.lock && rm -f /requirements.lock
COPY --from=builder /wheels /wheels
RUN python -m pip install --no-deps /wheels/*.whl && rm -rf /wheels

USER appuser
CMD ["python", "-m", "food_tracker"]
