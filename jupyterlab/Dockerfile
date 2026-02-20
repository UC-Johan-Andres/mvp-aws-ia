FROM python:3.11-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libpq-dev \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir \
    "jupyterlab>=4.0,<5.0" \
    psycopg2-binary \
    pymongo \
    sqlalchemy

RUN useradd -m -s /bin/bash jovyan && \
    mkdir -p /home/jovyan/work && \
    chown -R jovyan:jovyan /home/jovyan

USER jovyan
WORKDIR /home/jovyan/work

EXPOSE 8888

CMD ["jupyter", "lab", \
     "--ip=0.0.0.0", \
     "--no-browser", \
     "--ServerApp.allow_origin=*"]
