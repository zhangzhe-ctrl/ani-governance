-- Isolated acceptance only. Run explicitly by the task container administrator.
CREATE ROLE gov_acc_migrate LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
CREATE ROLE gov_acc_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
CREATE DATABASE gov_acc_data OWNER gov_acc_migrate;
REVOKE ALL ON DATABASE gov_acc_data FROM PUBLIC;
GRANT CONNECT ON DATABASE gov_acc_data TO gov_acc_runtime;
