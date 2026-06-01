from functools import lru_cache
from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    gcp_project_id: str = Field(...)
    gcp_location: str = Field(default="global")
    agent_id: str = Field(...)
    gcs_bucket: str = Field(...)
    cloud_tasks_queue: str = Field(...)
    cloud_tasks_location: str = Field(...)
    cloud_run_service_url: str = Field(default="")
    line_channel_secret: str = Field(...)
    line_channel_access_token: str = Field(...)
    use_firestore_emulator: bool = Field(default=False)
    firestore_emulator_host: str = Field(default="localhost:8081")


@lru_cache
def get_settings() -> Settings:
    return Settings()
