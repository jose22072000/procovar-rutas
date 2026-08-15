# Imagen del frontend. Solo interfaz: no toca la base ni guarda secretos.

FROM node:22-alpine AS dependencias
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci --omit=dev || npm install --omit=dev

FROM node:22-alpine AS construccion
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci || npm install
COPY . .
# La URL de la API se hornea en el bundle: es pública (la resuelve el navegador),
# no un secreto.
ARG NEXT_PUBLIC_API_URL
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
RUN npm run build

FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production
RUN adduser -D -u 10001 rutas

COPY --from=dependencias /app/node_modules ./node_modules
COPY --from=construccion /app/.next ./.next
COPY --from=construccion /app/public ./public
COPY --from=construccion /app/package.json ./package.json

USER rutas
EXPOSE 3601

CMD ["npx", "next", "start", "-p", "3601"]
