FROM python:3.12-slim
RUN pip install numpy pandas scikit-learn sympy
RUN apt-get update
RUN apt-get install -y psmisc

RUN adduser --system --home /home/app app
COPY runner.py /home/app

RUN adduser --system --home /mnt/data user
WORKDIR /mnt/data
USER user

CMD ["tail", "-f", "/dev/null"]