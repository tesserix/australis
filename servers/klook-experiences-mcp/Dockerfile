# syntax=docker/dockerfile:1.18
FROM ghcr.io/astral-sh/uv:0.12.5@sha256:e85be844203885286c60ffad8a858d48afb6c5a5c237ca0e67f12e74b8f174b1 AS uv
FROM ghcr.io/tesserix/base-python-adk-3.14:20260829@sha256:5a6fd1863ed7f37f3929cc596d0ec063c3077c11713cd334f14d1df2b30ef386 AS builder
USER root
COPY --from=uv /uv /bin/uv
ENV UV_PROJECT_ENVIRONMENT=/app/.venv
WORKDIR /app
COPY pyproject.toml uv.lock ./
COPY src ./src
RUN uv sync --frozen --no-dev --no-editable
FROM ghcr.io/tesserix/base-python-adk-3.14:20260829@sha256:5a6fd1863ed7f37f3929cc596d0ec063c3077c11713cd334f14d1df2b30ef386
WORKDIR /app
COPY --from=builder --chown=10001:10001 /app/.venv /app/.venv
USER 10001:10001
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
EXPOSE 8080
ENTRYPOINT ["/app/.venv/bin/python", "-m", "klook_experiences_mcp.main"]
