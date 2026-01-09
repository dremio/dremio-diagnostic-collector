pipeline {
    agent {
        kubernetes {
            agentInjection true
            defaultContainer 'agent'
            cloud 'kubernetes'
            yamlFile 'jenkins/agent.yaml'
        }
    }
    options {
        timeout(time: 20, unit: 'MINUTES')
    }
    stages {
        stage('Setup') {
            steps {
                sh 'apk add bash curl'
            }
        }
    }
}
