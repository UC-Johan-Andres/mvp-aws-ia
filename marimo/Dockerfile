FROM ghcr.io/marimo-team/marimo:latest-data

USER root

RUN apt-get update && apt-get install -y --no-install-recommends \
    libpq-dev \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir \
    psycopg2-binary \
    pymongo \
    sqlalchemy

RUN useradd -m -s /bin/bash marimo_user && \
    mkdir -p /home/marimo_user/notebooks && \
    chown -R marimo_user:marimo_user /home/marimo_user

USER marimo_user
WORKDIR /home/marimo_user/notebooks

EXPOSE 8080

CMD ["marimo", "edit", "--no-token", "-p", "8080", "--host", "0.0.0.0"]
