FROM python:3.14-slim AS builder

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/
WORKDIR /app
COPY pyproject.toml uv.lock ./
RUN uv pip sync

# slim runtime image
FROM python:3.12-slim
RUN addgroup --system appgroup && adduser --system --ingroup appgroup appuser

WORKDIR /app
COPY --from=builder /usr/local /usr/local
COPY faas-setup/ ./
COPY functions/${APP_FILE}.py ./

ENV HANDLER_DIR=.
ENV HANDLER_NAME=${APP_FILE}

USER appuser

EXPOSE 8000

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000", "--loop", "uvloop", "--http", "httptools"]