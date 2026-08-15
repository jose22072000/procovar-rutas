import type { NextConfig } from "next";

const config: NextConfig = {
  reactStrictMode: true,
  // El frontend no habla con la base: solo con la API de Go, que es quien
  // controla el alcance por rol.
  env: {
    NEXT_PUBLIC_API_URL: process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:3600",
  },
};

export default config;
