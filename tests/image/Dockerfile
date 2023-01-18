FROM openjdk:8-slim

RUN apt-get update && \
    apt-get install -y sudo && \
    apt-get install -y curl --no-install-recommends && \
    apt-get install -y wget && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY impala-4.1.0-SNAPSHOT.tar.gz /

ENV HADOOP_HOME="/opt/hadoop"
ENV HIVE_HOME="/opt/hive"
ENV HADOOP_CLASSPATH="${HADOOP_HOME}/share/hadoop/tools/lib/*:${HIVE_HOME}/lib"
ENV PATH="${HADOOP_HOME}/bin:${HIVE_HOME}/lib/:${HADOOP_HOME}/share/hadoop/tools/lib/:/opt/postgresql-jdbc.jar:${PATH}"

RUN sudo tar xzf impala-4.1.0-SNAPSHOT.tar.gz -P -C /
RUN sudo rm -r /impala-4.1.0-SNAPSHOT.tar.gz /opt/impala

RUN curl -s https://jdbc.postgresql.org/download/postgresql-42.2.16.jar -o postgresql-42.2.16.jar && mv postgresql-42.2.16.jar /opt/postgresql-jdbc.jar
RUN cp /opt/postgresql-jdbc.jar  ${HADOOP_HOME}/share/hadoop/common/lib/ && chmod -R 775 ${HADOOP_HOME}/share/hadoop/common/lib/*
RUN cp /opt/postgresql-jdbc.jar ${HIVE_HOME}/lib/ && chmod -R 775 ${HIVE_HOME}/lib/*

RUN wget https://github.com/kubernetesbigdataeg/propgen/releases/latest/download/propgen

COPY entrypoint.sh ${HIVE_HOME}/bin/
RUN chmod 775 ${HIVE_HOME}/bin/entrypoint.sh && \
    chmod 777 propgen

COPY hms-plugin.jar ${HIVE_HOME}/lib/

# Metastore URI Port
EXPOSE 9083

WORKDIR ${HIVE_HOME}
CMD ["bin/entrypoint.sh"]