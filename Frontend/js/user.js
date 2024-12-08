// Evento para el botón de logout
document.querySelector(".logout-button").addEventListener("click", () => {
    if (confirm("¿Estás seguro de que deseas cerrar sesión?")) {
        fetch("/api/logout", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
        })
            .then((response) => {
                if (response.ok) {
                    alert("Sesión cerrada exitosamente.");
                    window.location.href = "/"; // Redirige al usuario a la página de inicio o login
                } else {
                    alert("Error cerrando sesión.");
                }
            })
            .catch((err) => console.error("Error:", err));
    }
});