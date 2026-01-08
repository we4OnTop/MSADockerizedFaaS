FROM python:3.9-slim

ENV PYTHONUNBUFFERED=1
WORKDIR /app

# DIESE ZEILE IST ENTSCHEIDEND:
RUN pip install docker

COPY test.py .

CMD ["python", "test.py"]