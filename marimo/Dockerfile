# syntax=docker/dockerfile:1.4

FROM python:3.14-slim

COPY --from=ghcr.io/astral-sh/uv:0.4.20 /uv /bin/uv
ENV UV_SYSTEM_PYTHON=1
ENV PYTHONUNBUFFERED=1

WORKDIR /app

COPY --link requirements.txt .
RUN uv pip install --no-cache -r requirements.txt

COPY --link entrypoint.sh .
RUN chmod +x entrypoint.sh

RUN mkdir -p /root/notebooks

WORKDIR /root/notebooks

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
  CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
