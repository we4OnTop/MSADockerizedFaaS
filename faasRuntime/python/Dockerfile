FROM python:3.14-slim AS builder

# Copy uv binary from upstream image
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

WORKDIR /app

COPY pyproject.toml uv.lock ./

RUN uv export --frozen --format=requirements-txt > requirements.txt && \
    uv pip sync requirements.txt --system

ARG EXTRA_DEPS=""
RUN if [ -n "$EXTRA_DEPS" ]; then \
        uv pip install $EXTRA_DEPS; \
    fi

# slim runtime image
FROM python:3.12-slim
RUN addgroup --system appgroup && adduser --system --ingroup appgroup appuser

ARG APP_FILE=""
ARG FUNCTION_DATA_DIR=""

WORKDIR /app
COPY --from=builder /usr/local /usr/local
COPY faas-setup/ ./
COPY functions/${FUNCTION_DATA_DIR}/ ./

ENV HANDLER_DIR="."
ENV HANDLER_NAME=${APP_FILE}

#USER appuser

ENV PORT="5000"
EXPOSE ${PORT}

CMD uvicorn faas-wrapper-server:app --host 0.0.0.0 --port ${PORT} --http h11