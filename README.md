# Finance-Compliance

This project is the basic version designed to analyze policies and target content, identifying elements that do not adhere to the specified policy.

## Features
- **Policy Analysis:** Analyzes given content against specified policies to identify non-compliance.
- **CURL Support:** Offers a CURL request feature for submitting policy and webpage URL for analysis.

## Getting Started

### Prerequisites
- Docker installed on your machine.
- An OpenAPI key for authentication.

### Steps to Run Locally
1. Build the Docker image:
   ```
   docker build -t <name> .
   ```
2. Run the Docker container, replacing `<key>` with your OpenAPI key:
   ```
   docker run -e OPENAPI_KEY=<key> -p 8080:8080 <name>
   ```
3. Execute a CURL request:
   ```bash
   curl --location --request POST 'localhost:8080/compliance?policy=https%3A%2F%2Fdocs.stripe.com%2Ftreasury%2Fmarketing-treasury&webpage=https%3A%2F%2Fmercury.com%2F' \
--header 'Content-Type: application/json' 
   ```

## Architecture

The current API operates synchronously, which is not the ideal approach. It should instead accept requests for analysis, return a status code of 201 with a message indicating the request has been submitted, and then, after analysis, write the response to a file and communicate that file's path to the user. We utilize In-Memory Caching to hold information for the same policy and webpage if we have already analyzed it. This architecture is highly extendable for implementing distributed caching.

## Areas for Improvement
1. **Asynchronous Content Analysis:** Transition to an asynchronous model for analyzing content to improve efficiency.
2. **Distributed Caching with TTL:** Implement distributed caching mechanisms with Time-To-Live (TTL) settings for better scalability and performance.
3. **Expanded Test Coverage:** Increase the number of test cases to cover more scenarios and ensure robustness.
4. **Support for URL and File Inputs:** Enhance the system to accept both URLs and file paths in requests for greater flexibility.
