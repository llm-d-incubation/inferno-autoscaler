# vllm_emulator
Modeling of vllm for research purposes

Installation and usage instruction here: https://scientific-goldfish-3af.notion.site/VLLM-Emulator-3ce1cf43f11b445ead76d10500201b93?pvs=4

Slides: https://ibm-my.sharepoint.com/:p:/r/personal/chiragshetty_ibm_com/Documents/vllm_emulator.pptx?d=w60d5d6f8c6e64bb7a36908ee93caf7d1&csf=1&web=1&e=FMIM6d


Code Structure:

```
vllm_model.py - All relevant classes and modelling is here
experiment.py - instantiate the vLLM model instance and run offline inference to get statistics  (python experiment.py)
server.py     - instantiate a vLLM model instance and start a (local) server to use it - (uvicorn server:app)
logs/run.log  - logs from all runs are stored here. Refreshed on each new run
```

## Container Image

To build the container image running the emulator, run this command:

```sh
$ docker build -t vllme .
```

Then you can run it:

```sh
$ docker run -p 8000:80 vllme
```

`-p 8000:80` tells docker to publish the container port 80 to your host port 8000.
