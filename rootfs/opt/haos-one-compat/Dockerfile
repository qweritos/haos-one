FROM python:3.14-slim

WORKDIR /opt/haos-one-compat

COPY requirements.txt ./requirements.txt
RUN pip install --no-cache-dir -r requirements.txt

ADD . .

CMD ["python", "-m", "haos_one_compat", "--bus", "system"]
