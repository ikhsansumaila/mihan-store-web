# MihanStore - Simple Online Store

MihanStore is a simple online store application built with React (Frontend) and Go (Backend), deployed using Docker Compose.

## Features
- Product listing with categories (kerupuk, tepung, saos, sambal, sendok plastik, box hampers)
- Product search
- User registration and login (hardcoded, in-memory)
- Basic UI for browsing products

## Tech Stack
- **Frontend:** React.js, Axios, React Router DOM
- **Backend:** Go (Golang), Gorilla Mux, Gorilla Handlers
- **Deployment:** Docker, Docker Compose, Nginx

## Directory Structure
```
mihanstore/
├── backend/
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   └── main.go
├── frontend/
│   ├── Dockerfile
│   ├── nginx.conf
│   ├── package.json
│   ├── public/
│   │   ├── index.html
│   │   └── styles.css
│   ├── src/
│   │   ├── App.js
│   │   ├── index.js
│   │   ├── index.css
│   │   └── components/
│   │       ├── Login.js
│   │       └── Register.js
├── docker-compose.yml
├── .env.example
├── .gitignore
└── README.md
```

## Getting Started

### Prerequisites
- Docker and Docker Compose installed on your system.

### Installation
1.  **Clone the repository:**
    ```bash
    git clone <repository_url>
    cd mihanstore
    ```
2.  **Create `.env` file:**
    Copy the `.env.example` to `.env` and configure any necessary environment variables. For MihanStore, `REACT_APP_API_URL` should point to your backend.
    ```bash
    cp .env.example .env
    ```

3.  **Build and run with Docker Compose:**
    Navigate to the `mihanstore` root directory and run:
    ```bash
    docker compose up --build -d
    ```

    This will:
    - Build the Go backend image and start the `mihanstore_backend` container on port `8080`.
    - Build the React frontend image and start the `mihanstore_frontend` container on port `3000` (mapped from container's port 80).

## Accessing the Application
-   **Frontend:** Open your browser to `http://localhost:3000`
-   **Backend API:** Access directly at `http://localhost:8080` (for testing purposes, usually accessed via frontend's Nginx proxy)

## Nginx Configuration for Production (mihankids.my.id)

To serve MihanStore on `https://mihankids.my.id` (replacing the old Netdata config), you need to update your Nginx configuration.

Edit `/etc/nginx/sites-available/mihankids` to look like this:

```nginx
server {
    listen 80;
    server_name mihankids.my.id;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name mihankids.my.id;

    ssl_certificate /etc/letsencrypt/live/mihankids.my.id/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/mihankids.my.id/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    # Frontend (React App)
    location / {
        proxy_pass http://localhost:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_redirect off;
    }

    # Backend API (optional, can be proxied via frontend's Nginx)
    # If you want to expose backend directly on a subpath, e.g., /api/
    # location /api/ {
    #     proxy_pass http://localhost:8080/api/;
    #     proxy_set_header Host $host;
    #     proxy_set_header X-Real-IP $remote_addr;
    #     proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    #     proxy_set_header X-Forwarded-Proto $scheme;
    # }
}
```

After updating the Nginx configuration, test the config and reload Nginx:
```bash
sudo nginx -t
sudo systemctl reload nginx
```

## Development

### Frontend
```bash
cd frontend
npm install
npm start
```

### Backend
```bash
cd backend
go mod tidy
go run main.go
```

