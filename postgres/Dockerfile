FROM postgres:15-alpine AS builder
RUN apk add --no-cache build-base clang llvm-dev git postgresql-dev \
    && ln -sf $(which clang) /usr/bin/clang-19
RUN git clone --branch v0.8.0 https://github.com/pgvector/pgvector.git
WORKDIR /pgvector
RUN make && make install

FROM postgres:15-alpine
COPY --from=builder /usr/local/lib/postgresql/vector.so /usr/local/lib/postgresql/
COPY --from=builder /usr/local/share/postgresql/extension/vector* /usr/local/share/postgresql/extension/
