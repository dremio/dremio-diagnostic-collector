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
        stage('Release') {
            steps {
                withVault(vaultSecrets: [[
                    path: 'secret/support/private/ddc-gh-pat', 
                    secretValues: [
                        [envVar: 'GITHUB_RELEASE_TOKEN', vaultKey: 'ddc-gh-pat'],
                    ]
                ]]) {
                    echo "Can use secret $GITHUB_RELEASE_TOKEN"
                }
            }
        }
    }
}
