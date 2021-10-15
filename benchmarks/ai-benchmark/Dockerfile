FROM tensorflow/tensorflow:2.4.1-gpu

RUN apt-get update && apt-get install -y --no-install-recommends apt-utils

RUN pip install --upgrade pip

RUN apt-get -y install git
RUN git clone -b feat/transformer https://github.com/shiyoubun/ai-benchmark.git

WORKDIR ai-benchmark
RUN pip install -e .

ENTRYPOINT [ "python", "bin/ai-benchmark.py" ]
