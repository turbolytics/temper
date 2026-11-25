FROM python:3.12-slim
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && rm -rf /var/lib/apt/lists/*
COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt
# PostgreSQL's source at PG_TAG: the docs, the regression tests and errcodes.txt
# are what the inventory and the corpus are generated from.
COPY PG_TAG /tmp/PG_TAG
RUN tag=$(cat /tmp/PG_TAG) && mkdir /pg && \
    curl -fsSL "https://github.com/postgres/postgres/archive/refs/tags/${tag}.tar.gz" \
    | tar -xz -C /pg --strip-components=1 \
        "postgres-${tag}/doc/src/sgml" "postgres-${tag}/src/test/regress" "postgres-${tag}/src/backend/utils/errcodes.txt"
WORKDIR /oracle
ENV PYTHONPATH=/oracle PYTHONDONTWRITEBYTECODE=1
ENTRYPOINT ["/oracle/oracle"]
