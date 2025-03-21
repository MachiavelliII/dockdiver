#!/bin/bash

# Check for required tools and install if requested
check_tools() {
    # Check if docker is installed
    if ! command -v docker &> /dev/null; then
        echo -e "\e[31m[-]\e[0m Docker is not installed!"
        read -p "Would you like to install Docker? (y/n): " answer
        if [ "$answer" = "y" ] || [ "$answer" = "Y" ]; then
            echo -e "\e[33m[*]\e[0m Installing Docker..."
            sudo apt-get update
            sudo apt-get install -y docker.io
            # Verify installation
            if ! command -v docker &> /dev/null; then
                echo -e "\e[31m[-]\e[0m Docker installation failed. Please install manually."
                exit 1
            fi
        else
            echo -e "\e[31m[-]\e[0m Lab cannot start without Docker. Exiting..."
            exit 1
        fi
    fi

    # Check if docker-compose is installed
    if ! command -v docker-compose &> /dev/null; then
        echo -e "\e[31m[-]\e[0m Docker Compose is not installed!"
        read -p "Would you like to install Docker Compose? (y/n): " answer
        if [ "$answer" = "y" ] || [ "$answer" = "Y" ]; then
            echo -e "\e[33m[*]\e[0m Installing Docker Compose..."
            sudo apt-get update
            sudo apt-get install -y docker-compose
            # Verify installation
            if ! command -v docker-compose &> /dev/null; then
                echo -e "\e[31m[-]\e[0m Docker Compose installation failed. Please install manually."
                exit 1
            fi
        else
            echo -e "\e[31m[-]\e[0m Lab cannot start without Docker Compose. Exiting..."
            exit 1
        fi
    fi
}

# Check tools before proceeding
check_tools

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
