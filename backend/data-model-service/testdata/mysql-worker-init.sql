-- Only mounted into the tmpfs MySQL fixture by compose.mysql-worker-test.yaml.
-- Owner test objects are created in generated databases by the existing helpers.
CREATE DATABASE decision_platform_test CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
