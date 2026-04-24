import time
import json
import uuid
import argparse
import requests
from datetime import datetime

def now():
    return time.time()

def send_request(url, model, prompt, stream):
    t0 = now()
    r = requests.post(
        url,
        json={
            "model": model,
            "messages": [{"role": "user", "content": prompt}],
            "stream": stream,
        },
        stream=stream,
    )
    r.raise_for_status()
    if not stream:
        raise RuntimeError("Use stream=True for token timestamps")

    first_token_time = None
    token_times = []
    usage = None

    for line in r.iter_lines():
        if not line:
            continue
        if not line.startswith(b"data: "):
            continue

        payload = line[len(b"data: "):].strip()
        if payload == b"[DONE]":
            break

        try:
            chunk = json.loads(payload)
            t = now()
            if first_token_time is None:
                first_token_time = t
            token_times.append(t)
            if "usage" in chunk:
                usage = chunk["usage"]
        except:
            continue

    t_end = now()
    return {
        "t0": t0,
        "t_first": first_token_time,
        "t_tokens": token_times,
        "t_end": t_end,
        "usage": usage,
    }


def main():
    parser = argparse.ArgumentParser(description="vLLM benchmark client")
    parser.add_argument("--vllm-url", default="http://127.0.0.1:8000/v1/chat/completions",
                        help="vLLM OpenAI-compatible endpoint")
    parser.add_argument("--model", default="Qwen/Qwen3-8B",
                        help="Model name registered on the vLLM server")
    parser.add_argument("--prompt", default="Explain the difference between supervised and unsupervised learning.",
                        help="Prompt text for each request")
    parser.add_argument("--warmup", type=int, default=30,
                        help="Number of warmup requests (default: 30)")
    parser.add_argument("--runs", type=int, default=200,
                        help="Number of benchmark requests (default: 200)")
    parser.add_argument("--output", default=None,
                        help="Output file path (default: auto-generated)")
    args = parser.parse_args()

    fname = args.output or f"bench_{uuid.uuid4().hex}.jsonl"

    print(f"vLLM URL:    {args.vllm_url}")
    print(f"Model:       {args.model}")
    print(f"Warmup:      {args.warmup}")
    print(f"Runs:        {args.runs}")
    print(f"Output:      {fname}")
    print()

    print("warmup...")
    for i in range(args.warmup):
        send_request(args.vllm_url, args.model, args.prompt, stream=True)
        if (i + 1) % 10 == 0:
            print(f"  {i + 1}/{args.warmup}")

    print("running...")
    with open(fname, "w") as f:
        for i in range(args.runs):
            data = send_request(args.vllm_url, args.model, args.prompt, stream=True)
            f.write(json.dumps(data) + "\n")
            if (i + 1) % 10 == 0:
                print(f"  {i + 1}/{args.runs}")

    print("saved:", fname)


if __name__ == "__main__":
    main()
