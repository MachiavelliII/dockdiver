#!/bin/bash

cd lab/

# Stop any existing lab
echo -e "\e[33m[*]\e[0m Stopping and removing any existing lab for fresh start...\n"
sudo docker-compose down && sudo rm -rf registry-data
sleep 1

# Start the lab
echo -e "\n\e[32m[+]\e[0m Starting the Docker registry lab on localhost:5000...\n"
sudo docker-compose up -d
sleep 2

# Tag and push the test image
echo -e "\n\e[32m[+]\e[0m Tagging test-ubuntu image..."
sudo docker tag test-ubuntu:latest localhost:5000/test-ubuntu:latest
sleep 2

echo -e "\n\e[32m[+]\e[0m Pushing test-ubuntu to localhost:5000...\n"
sudo docker push localhost:5000/test-ubuntu:latest
sleep 2

# Verify the registry
echo -e "\n\e[32m[+]\e[0m Verifying the registry with curl...\n"
curl http://localhost:5000/v2/_catalog
sleep 1