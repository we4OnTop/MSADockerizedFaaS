FROM python:3.12-slim AS builder

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

WORKDIR /app

COPY ./orchestration/pyproject.toml ./orchestration/uv.lock ./

RUN uv export --frozen --format=requirements-txt > requirements.txt && \
    uv pip sync requirements.txt --system


FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1

WORKDIR /app

COPY --from=builder /usr/local /usr/local
COPY ./orchestration/*.py ./
COPY ./../faasRuntime/global-function-definition.yml ./

CMD ["python", "-u", "main.py"]
