FROM node:22-alpine AS assets
WORKDIR /assets
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --omit=dev

FROM nginx:1.30.6-alpine
COPY --from=assets /assets/node_modules/swagger-ui-dist/swagger-ui.css /usr/share/nginx/html/swagger/swagger-ui.css
COPY --from=assets /assets/node_modules/swagger-ui-dist/swagger-ui-bundle.js /usr/share/nginx/html/swagger/swagger-ui-bundle.js
