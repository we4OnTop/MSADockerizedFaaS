FROM python:3.14-slim AS builder

# Copy uv binary from upstream image
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

WORKDIR /app

COPY pyproject.toml uv.lock ./

RUN uv pip sync

ARG EXTRA_DEPS=""
RUN if [ -n "$EXTRA_DEPS" ]; then \
        uv pip install $EXTRA_DEPS; \
    fi

# slim runtime image
FROM python:3.12-slim
RUN addgroup --system appgroup && adduser --system --ingroup appgroup appuser

ARG APP_FILE=""

WORKDIR /app
COPY --from=builder /usr/local /usr/local
COPY faas-setup/ ./
COPY functions/${APP_FILE}.py ./

ARG HANDLER_DIR="."
ARG HANDLER_NAME=${APP_FILE}

USER appuser

ENV PORT="8000"
EXPOSE ${PORT}

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "${PORT}", "--loop", "uvloop", "--http", "httptools"]