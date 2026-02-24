FROM python:3.11-slim

COPY --from=ghcr.io/astral-sh/uv:0.4.20 /uv /bin/uv
ENV UV_SYSTEM_PYTHON=1

USER root

RUN apt-get update && apt-get install -y --no-install-recommends \
    libpq-dev \
    && rm -rf /var/lib/apt/lists/*

RUN uv pip install --no-cache \
    "marimo" \
    "websockets<14" \
    pandas \
    psycopg2-binary \
    pymongo \
    sqlalchemy

RUN useradd -m -s /bin/bash marimo_user && \
    mkdir -p /home/marimo_user/notebooks && \
    chown -R marimo_user:marimo_user /home/marimo_user

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

USER marimo_user
WORKDIR /home/marimo_user/notebooks

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
