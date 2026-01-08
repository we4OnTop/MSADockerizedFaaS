FROM python:3.9-slim

WORKDIR /app

RUN pip install requests

COPY ./statsd_trigger.py .

CMD ["python", "-u", "statsd_trigger.py"]