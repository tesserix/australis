# syntax=docker/dockerfile:1.18
FROM ghcr.io/astral-sh/uv:0.12.5@sha256:e85be844203885286c60ffad8a858d48afb6c5a5c237ca0e67f12e74b8f174b1 AS uv
FROM ghcr.io/tesserix/base-python-adk-3.14:20260912@sha256:4cbbea807dc3af7664824e7e270a9618eaa49cb16806c83d58236ce3c2aba523 AS builder
USER root
COPY --from=uv /uv /bin/uv
ENV UV_PROJECT_ENVIRONMENT=/app/.venv
WORKDIR /app
COPY pyproject.toml uv.lock ./
COPY src ./src
RUN uv sync --frozen --no-dev --no-editable
FROM ghcr.io/tesserix/base-python-adk-3.14:20260912@sha256:4cbbea807dc3af7664824e7e270a9618eaa49cb16806c83d58236ce3c2aba523
WORKDIR /app
COPY --from=builder --chown=10001:10001 /app/.venv /app/.venv
USER 10001:10001
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
EXPOSE 8080
ENTRYPOINT ["/app/.venv/bin/python", "-m", "booking_demand_mcp.main"]
