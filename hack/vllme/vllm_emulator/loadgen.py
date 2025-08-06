import json
import time
import random
import threading
import argparse
from typing import Union, Tuple, List
from openai import OpenAI


def get_current_rate(elapsed: float, rate_spec: Union[float, list[Tuple[float, float]]]) -> float:
    if isinstance(rate_spec, float):
        return rate_spec
    time_marker = 0
    for duration, rpm in rate_spec:
        if elapsed <= time_marker + duration:
            return rpm
        time_marker += duration
    return 0.0  # After all scheduled periods


# Function to simulate a single client sending a request
def send_request(client, model, content_length):
    content = "x" * content_length

    try:
        chat_completion = client.chat.completions.create(
            messages=[
                {"role": "user", "content": content}
            ],
            model=model,
        )
        # Uncomment below to print API response
        # print(chat_completion.choices[0].message.content)
    except Exception as e:
        print(f"Request failed: {e}")

# Function to simulate Poisson process and generate requests
def poisson_request_generator(client, model, rate, content_length):
    client_threads = []
    request_count = 0
    start_time = time.time()
    last_report_time = start_time
    total_duration = 0

    if isinstance(rate, list):
        for duration, _ in rate:
            total_duration += duration
    else:
        total_duration = float("inf")

    try:
        while time.time() - start_time <= total_duration:
            elapsed = time.time() - start_time
            current_rpm = get_current_rate(elapsed, rate)
            if current_rpm <= 0:
                break # stop sending requests

            mean_interval = 60.0 / current_rpm
            inter_arrival_time = random.expovariate(1 / mean_interval)
            time.sleep(inter_arrival_time)

            thread = threading.Thread(target=send_request, args=(client, model, content_length))
            thread.start()
            client_threads.append(thread)

            request_count += 1

            if time.time() - last_report_time >= 60:
                print(f"Total requests sent in the last minute: {request_count}")
                request_count = 0
                last_report_time = time.time()

        if request_count > 0:
             print(f"Total requests sent in the last minute: {request_count}")

        print("Load generation finished. Waiting for all threads to complete...")

        for thread in client_threads:
            thread.join()

    except KeyboardInterrupt:
        print("\nLoad generator stopped by user.")

def deterministic_request_generator(client, model, rate, content_length):
    client_threads = []
    request_count = 0
    start_time = time.time()
    last_report_time = start_time
    total_duration = 0

    if isinstance(rate, list):
        for duration, _ in rate:
            total_duration += duration
    else:
        # For a constant rate, you might need a different way to stop,
        # perhaps an explicit --duration argument. For now, let's assume
        # the user will stop it with Ctrl-C.
        total_duration = float('inf')
    
    try:
        while time.time() - start_time <= total_duration:
            elapsed = time.time() - start_time
            current_rpm = get_current_rate(elapsed, rate)
            if current_rpm <= 0:
                break # stop sending requests

            sleep_interval = 60.0 / current_rpm
            time.sleep(sleep_interval)

            thread = threading.Thread(target=send_request, args=(client, model, content_length))
            thread.start()
            client_threads.append(thread)

            request_count += 1

            if time.time() - last_report_time >= 60:
                print(f"Total requests sent in the last minute: {request_count}")
                request_count = 0
                last_report_time = time.time()
        
        if request_count > 0:
             print(f"Total requests sent in the last minute: {request_count}")

        print("Load generation finished. Waiting for all threads to complete...")

        for thread in client_threads:
            thread.join()

    except KeyboardInterrupt:
        print("\nLoad generator stopped by user.")


def parse_request_rate(arg: str) -> Union[float, list[tuple[float, float]]]:
    """Parses the request rate argument from CLI. Supports float or JSON list."""
    try:
        return float(arg)  # If a single float is passed, return it as a constant rate
    except ValueError:
        return json.loads(arg)  # If a JSON string is passed, parse it as a list


def main():
    parser = argparse.ArgumentParser(description="OpenAI-compatible load generator")
    parser.add_argument(
        "--api-key", 
        default="fake-api-key", 
        help="OpenAI API key (default: fake-api-key)"
    )
    parser.add_argument(
        "--url",
        type=str,
        default="http://localhost:8000/v1",
        help="Base URL for the OpenAI-compatible server (default: http://localhost:30000/v1)"
    )
    parser.add_argument(
        "--model", 
        default="gpt-1337-turbo-pro-max", 
        help="Model name"
    )
    parser.add_argument(
        "--content-length", 
        type=int,
        default=150, 
        help="Length of prompt content"
    )
    parser.add_argument(
        "--seed", 
        type=int, 
        default=None, 
        help="Seed for random number generator"
    )
    parser.add_argument(
        "--mode", 
        choices=["poisson", "deterministic"], 
        default="deterministic", 
        help="Distribution of request arrivals"
    )
    parser.add_argument(
        "--rate",
        type=parse_request_rate,
        default=float("60"), # Default to 60 requests per minute
        help=(
            "Number of requests per second. Can be either:\n"
            "  - A single float (e.g., '4.0') for a constant rate.\n"
            "  - A JSON string list (e.g., '[[60, 4], [60, 6], [60, 8]]') "
            "for a dynamically changing rate."
        ),
    )

    args = parser.parse_args()

    # Set seed for reproducibility
    if args.seed is not None:
        random.seed(args.seed)
        print(f"Random seed set to: {args.seed}")

    print(f"Starting load generator with {args.mode} mode")
    print(f"Server Address: {args.url}")
    print(f"Request Rate = {args.rate}")
    print(f"Model: {args.model}")
    print(f"Content Length: {args.content_length}")
    print(f"API Key: {args.api_key}")

    client = OpenAI(api_key=args.api_key, base_url=args.url)

    if args.mode == "poisson":
        poisson_request_generator(
            client=client,
            model=args.model,
            rate=args.rate,
            content_length=args.content_length,
        )
    else:
        deterministic_request_generator(
            client=client,
            model=args.model,
            rate=args.rate,
            content_length=args.content_length,
        )


if __name__ == "__main__":
    main()
