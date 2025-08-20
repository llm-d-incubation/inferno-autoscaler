FROM python:3.12-slim

# Install system dependencies if any
# RUN apt-get update && apt-get install -y --no-install-recommends \
#     ...

# Set a working directory
WORKDIR /app

# Install the necessary Python packages
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy the script or code if needed
# COPY . .

# Set a default command to keep the container running for interactive sessions
CMD ["tail", "-f", "/dev/null"]