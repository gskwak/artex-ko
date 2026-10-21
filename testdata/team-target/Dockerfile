FROM python:3.12-alpine

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app
COPY server.py /app/server.py

RUN addgroup -S lab && adduser -S -G lab -u 10001 lab \
    && mkdir /logs && chown lab:lab /logs

USER lab
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=10s --start-period=10s --retries=5 \
  CMD python -c "from urllib.request import urlopen; urlopen('http://127.0.0.1:8080/health', timeout=5).close()"

CMD ["python", "/app/server.py"]
